package households

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"budgettracker/internal/auth"
	"budgettracker/internal/store"
)

// InvitationLifetime is how long a new invitation link remains valid.
const InvitationLifetime = 7 * 24 * time.Hour

var (
	// ErrInvitationNotFound means the token or invitation does not exist.
	ErrInvitationNotFound = errors.New("invitation not found")
	// ErrInvitationExpired means the invitation is past its expiry.
	ErrInvitationExpired = errors.New("invitation expired")
	// ErrInvitationAccepted means the invitation was already used.
	ErrInvitationAccepted = errors.New("invitation already accepted")
	// ErrInvitationEmailMismatch means the signed-in account email differs.
	ErrInvitationEmailMismatch = errors.New("invitation email mismatch")
	// ErrInvitationExists means a pending invitation already exists for the
	// same household and email.
	ErrInvitationExists = errors.New("pending invitation already exists")
	// ErrAlreadyMember means the invited email already belongs to the household.
	ErrAlreadyMember = errors.New("already a household member")
)

// Invitation is a pending or accepted household invitation. The raw token is
// never stored; CreateInvitation returns it exactly once.
type Invitation struct {
	ID              uuid.UUID
	HouseholdID     uuid.UUID
	HouseholdName   string
	Email           string
	Role            Role
	InvitedByUserID uuid.UUID
	ExpiresAt       time.Time
	AcceptedAt      *time.Time
	CreatedAt       time.Time
}

// CreateInvitationParams contains invite input.
type CreateInvitationParams struct {
	HouseholdID uuid.UUID
	ActorID     uuid.UUID
	Email       string
	Role        Role
}

// CreateInvitation creates a single-use invitation and returns its raw token.
// Only household admins may invite.
func (s *Service) CreateInvitation(ctx context.Context, params CreateInvitationParams) (Invitation, string, error) {
	if _, err := s.adminRole(ctx, params.HouseholdID, params.ActorID); err != nil {
		return Invitation{}, "", err
	}

	role := params.Role
	if role != RoleAdmin && role != RoleMember {
		return Invitation{}, "", &ValidationError{Field: "role", Message: "Choose admin or member."}
	}
	email, err := auth.NormalizeEmail(params.Email)
	if err != nil {
		return Invitation{}, "", &ValidationError{Field: "email", Message: "Enter a valid email address."}
	}

	alreadyMember, err := s.isMemberEmail(ctx, params.HouseholdID, email)
	if err != nil {
		return Invitation{}, "", err
	}
	if alreadyMember {
		return Invitation{}, "", ErrAlreadyMember
	}
	name, err := s.householdName(ctx, params.HouseholdID)
	if err != nil {
		return Invitation{}, "", err
	}

	expiresAt := time.Now().UTC().Add(InvitationLifetime)
	var invitation Invitation
	var rawToken string
	err = store.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			DELETE FROM household_invitations
			WHERE household_id = $1 AND email_normalized = $2
			  AND accepted_at IS NULL AND expires_at <= now()`,
			params.HouseholdID, email,
		); err != nil {
			return fmt.Errorf("remove expired invitations: %w", err)
		}

		raw, hash, err := newInvitationToken()
		if err != nil {
			return err
		}
		rawToken = raw
		invitation = Invitation{
			HouseholdID:     params.HouseholdID,
			HouseholdName:   name,
			Email:           email,
			Role:            role,
			InvitedByUserID: params.ActorID,
			ExpiresAt:       expiresAt,
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO household_invitations
				(household_id, email_normalized, role, invited_by_user_id, token_hash, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, created_at`,
			params.HouseholdID, email, role, params.ActorID, hash, expiresAt,
		).Scan(&invitation.ID, &invitation.CreatedAt)
		if store.IsUniqueViolation(err, "household_invitations_pending_unique") {
			return ErrInvitationExists
		}
		if err != nil {
			return fmt.Errorf("insert invitation: %w", err)
		}
		return nil
	})
	if err != nil {
		return Invitation{}, "", err
	}
	return invitation, rawToken, nil
}

// ListInvitations lists pending invitations for an admin.
func (s *Service) ListInvitations(ctx context.Context, actorID, householdID uuid.UUID) ([]Invitation, error) {
	if _, err := s.adminRole(ctx, householdID, actorID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.household_id, h.name, i.email_normalized, i.role,
		       i.invited_by_user_id, i.expires_at, i.accepted_at, i.created_at
		FROM household_invitations i
		JOIN households h ON h.id = i.household_id
		WHERE i.household_id = $1 AND i.accepted_at IS NULL
		ORDER BY i.created_at DESC`,
		householdID,
	)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	defer rows.Close()

	var invitations []Invitation
	for rows.Next() {
		invitation, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invitations: %w", err)
	}
	return invitations, nil
}

// InvitationByToken looks up an invitation for the public acceptance page.
func (s *Service) InvitationByToken(ctx context.Context, rawToken string) (Invitation, error) {
	invitation, err := s.invitationByHash(ctx, hashInvitationToken(rawToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrInvitationNotFound
	}
	if err != nil {
		return Invitation{}, err
	}
	return invitation, nil
}

// AcceptInvitation validates the token, email match, and expiry, then creates
// the membership and marks the invitation accepted in one transaction.
func (s *Service) AcceptInvitation(ctx context.Context, rawToken string, userID uuid.UUID) (Invitation, error) {
	hash := hashInvitationToken(rawToken)
	var invitation Invitation
	err := store.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			SELECT i.id, i.household_id, h.name, i.email_normalized, i.role,
			       i.invited_by_user_id, i.expires_at, i.accepted_at, i.created_at
			FROM household_invitations i
			JOIN households h ON h.id = i.household_id
			WHERE i.token_hash = $1
			FOR UPDATE OF i`, hash)
		var err error
		invitation, err = scanInvitation(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvitationNotFound
		}
		if err != nil {
			return err
		}
		if invitation.AcceptedAt != nil {
			return ErrInvitationAccepted
		}
		if !time.Now().UTC().Before(invitation.ExpiresAt) {
			return ErrInvitationExpired
		}

		var email string
		if err := tx.QueryRow(ctx,
			"SELECT email_normalized FROM auth_credentials WHERE user_id = $1",
			userID,
		).Scan(&email); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrInvitationEmailMismatch
			}
			return fmt.Errorf("look up accepting account: %w", err)
		}
		if email != invitation.Email {
			return ErrInvitationEmailMismatch
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO household_memberships (household_id, user_id, role)
			VALUES ($1, $2, $3)`,
			invitation.HouseholdID, userID, invitation.Role,
		)
		if store.IsUniqueViolation(err, "household_memberships_pkey") {
			return ErrAlreadyMember
		}
		if err != nil {
			return fmt.Errorf("insert invited membership: %w", err)
		}

		now := time.Now().UTC()
		if _, err := tx.Exec(ctx,
			"UPDATE household_invitations SET accepted_at = $2 WHERE id = $1",
			invitation.ID, now,
		); err != nil {
			return fmt.Errorf("mark invitation accepted: %w", err)
		}
		invitation.AcceptedAt = &now
		return nil
	})
	if err != nil {
		return Invitation{}, err
	}
	return invitation, nil
}

// RevokeInvitation removes a pending invitation as an admin of the path
// household. The invitation must belong to that household; a mismatched pair
// is reported as not found without revealing the real household. Acceptance
// and revocation serialize on the invitation row.
func (s *Service) RevokeInvitation(
	ctx context.Context,
	actorID, householdID, invitationID uuid.UUID,
) error {
	if _, err := s.adminRole(ctx, householdID, actorID); err != nil {
		return err
	}

	return store.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var acceptedAt *time.Time
		err := tx.QueryRow(ctx, `
			SELECT accepted_at
			FROM household_invitations
			WHERE id = $1 AND household_id = $2
			FOR UPDATE`,
			invitationID, householdID,
		).Scan(&acceptedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvitationNotFound
		}
		if err != nil {
			return fmt.Errorf("look up invitation: %w", err)
		}
		if acceptedAt != nil {
			return ErrInvitationAccepted
		}

		tag, err := tx.Exec(ctx, `
			DELETE FROM household_invitations
			WHERE id = $1 AND household_id = $2 AND accepted_at IS NULL`,
			invitationID, householdID,
		)
		if err != nil {
			return fmt.Errorf("delete invitation: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrInvitationNotFound
		}
		return nil
	})
}

func (s *Service) adminRole(ctx context.Context, householdID, userID uuid.UUID) (Role, error) {
	role, err := s.membershipRole(ctx, householdID, userID)
	if err != nil {
		return "", err
	}
	if role != RoleAdmin {
		return "", ErrForbidden
	}
	return role, nil
}

func (s *Service) membershipRole(ctx context.Context, householdID, userID uuid.UUID) (Role, error) {
	var role string
	err := s.pool.QueryRow(ctx, `
		SELECT role FROM household_memberships
		WHERE household_id = $1 AND user_id = $2`,
		householdID, userID,
	).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("look up household membership: %w", err)
	}
	return Role(role), nil
}

func (s *Service) isMemberEmail(ctx context.Context, householdID uuid.UUID, email string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM auth_credentials c
			JOIN household_memberships m ON m.user_id = c.user_id
			WHERE c.email_normalized = $1 AND m.household_id = $2
		)`, email, householdID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check existing member: %w", err)
	}
	return exists, nil
}

func (s *Service) householdName(ctx context.Context, householdID uuid.UUID) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, "SELECT name FROM households WHERE id = $1", householdID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("look up household name: %w", err)
	}
	return name, nil
}

func (s *Service) invitationByHash(ctx context.Context, hash string) (Invitation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT i.id, i.household_id, h.name, i.email_normalized, i.role,
		       i.invited_by_user_id, i.expires_at, i.accepted_at, i.created_at
		FROM household_invitations i
		JOIN households h ON h.id = i.household_id
		WHERE i.token_hash = $1`, hash)
	return scanInvitation(row)
}

func (s *Service) invitationByID(ctx context.Context, id uuid.UUID) (Invitation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT i.id, i.household_id, h.name, i.email_normalized, i.role,
		       i.invited_by_user_id, i.expires_at, i.accepted_at, i.created_at
		FROM household_invitations i
		JOIN households h ON h.id = i.household_id
		WHERE i.id = $1`, id)
	invitation, err := scanInvitation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invitation{}, ErrNotFound
	}
	if err != nil {
		return Invitation{}, err
	}
	return invitation, nil
}

func scanInvitation(row pgx.Row) (Invitation, error) {
	var invitation Invitation
	var role string
	var acceptedAt *time.Time
	if err := row.Scan(
		&invitation.ID, &invitation.HouseholdID, &invitation.HouseholdName,
		&invitation.Email, &role, &invitation.InvitedByUserID,
		&invitation.ExpiresAt, &acceptedAt, &invitation.CreatedAt,
	); err != nil {
		return Invitation{}, err
	}
	invitation.Role = Role(role)
	invitation.AcceptedAt = acceptedAt
	return invitation, nil
}

func newInvitationToken() (raw, hash string, err error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", "", fmt.Errorf("generate invitation token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(token)
	return raw, hashInvitationToken(raw), nil
}

func hashInvitationToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
