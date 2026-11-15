// Package households owns household membership, roles, and settings.
package households

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgettracker/internal/auth"
	"budgettracker/internal/store"
)

// Role is a household-specific membership role.
type Role string

const (
	// RoleAdmin can manage household settings, invitations, and household-wide
	// data.
	RoleAdmin Role = "admin"
	// RoleMember can manage their own entries and has personal visibility.
	RoleMember Role = "member"
)

var (
	// ErrNotFound means the household does not exist or the user is not a member.
	ErrNotFound = errors.New("household not found")
	// ErrForbidden means the user is a member but lacks the required role.
	ErrForbidden = errors.New("household action forbidden")
)

// ValidationError describes an invalid household field.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements error.
func (e *ValidationError) Error() string {
	return e.Message
}

// Household is a household visible to a user with that user's role.
type Household struct {
	ID        uuid.UUID
	Name      string
	Role      Role
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsAdmin reports whether the user has the admin role in this household.
func (h Household) IsAdmin() bool {
	return h.Role == RoleAdmin
}

// Member is a household member with their public profile and role.
type Member struct {
	UserID    uuid.UUID
	FirstName string
	LastName  string
	Email     string
	Role      Role
}

// Options are the household-level defaults. Currency codes become foreign keys
// when the currencies table is introduced.
type Options struct {
	DefaultIncomeCurrencyCode   string
	DefaultSpendingCurrencyCode string
	DefaultDisplayCurrencyCode  string
}

// Service owns household persistence and authorization checks.
type Service struct {
	pool *pgxpool.Pool
}

// NewService constructs a household service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// CreateAndActivate creates a household, its first admin membership, and
// default options, then makes it the current session's active household. All
// writes commit together or roll back together, and an invalid or missing
// session rolls back the new household.
func (s *Service) CreateAndActivate(
	ctx context.Context,
	userID uuid.UUID,
	sessionTokenHash, name string,
) (Household, error) {
	name, err := validateName(name)
	if err != nil {
		return Household{}, err
	}
	if sessionTokenHash == "" {
		return Household{}, auth.ErrSessionNotFound
	}

	var household Household
	err = store.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO households (name)
			VALUES ($1)
			RETURNING id, name, created_at, updated_at`,
			name,
		).Scan(&household.ID, &household.Name, &household.CreatedAt, &household.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert household: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO household_memberships (household_id, user_id, role)
			VALUES ($1, $2, $3)`,
			household.ID, userID, RoleAdmin,
		); err != nil {
			return fmt.Errorf("insert admin membership: %w", err)
		}

		if _, err := tx.Exec(ctx, "INSERT INTO household_options (household_id) VALUES ($1)", household.ID); err != nil {
			return fmt.Errorf("insert household options: %w", err)
		}

		tag, err := tx.Exec(ctx, `
			UPDATE sessions
			SET active_household_id = $3
			WHERE token_hash = $1 AND user_id = $2`,
			sessionTokenHash, userID, household.ID,
		)
		if err != nil {
			return fmt.Errorf("activate household: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return auth.ErrSessionNotFound
		}

		household.Role = RoleAdmin
		return nil
	})
	if err != nil {
		return Household{}, err
	}
	return household, nil
}

// List returns the households the user belongs to in creation order.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Household, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT h.id, h.name, m.role, h.created_at, h.updated_at
		FROM households h
		JOIN household_memberships m ON m.household_id = h.id
		WHERE m.user_id = $1
		ORDER BY h.created_at, h.id`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list households: %w", err)
	}
	defer rows.Close()

	var households []Household
	for rows.Next() {
		household, err := scanHousehold(rows)
		if err != nil {
			return nil, err
		}
		households = append(households, household)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate households: %w", err)
	}
	return households, nil
}

// Get returns a household only when the user is a member.
func (s *Service) Get(ctx context.Context, userID, householdID uuid.UUID) (Household, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT h.id, h.name, m.role, h.created_at, h.updated_at
		FROM households h
		JOIN household_memberships m ON m.household_id = h.id
		WHERE h.id = $1 AND m.user_id = $2`,
		householdID, userID,
	)
	return scanHousehold(row)
}

// Rename updates a household name. Only admins may rename a household.
func (s *Service) Rename(ctx context.Context, userID, householdID uuid.UUID, name string) (Household, error) {
	name, err := validateName(name)
	if err != nil {
		return Household{}, err
	}

	current, err := s.Get(ctx, userID, householdID)
	if err != nil {
		return Household{}, err
	}
	if !current.IsAdmin() {
		return Household{}, ErrForbidden
	}

	row := s.pool.QueryRow(ctx, `
		UPDATE households
		SET name = $2, updated_at = now()
		WHERE id = $1
		RETURNING id, name, created_at, updated_at`,
		householdID, name,
	)
	var household Household
	err = row.Scan(&household.ID, &household.Name, &household.CreatedAt, &household.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Household{}, ErrNotFound
	}
	if err != nil {
		return Household{}, fmt.Errorf("rename household: %w", err)
	}
	household.Role = RoleAdmin
	return household, nil
}

// Members lists household members. Only admins may access the member list.
func (s *Service) Members(ctx context.Context, userID, householdID uuid.UUID) ([]Member, error) {
	current, err := s.Get(ctx, userID, householdID)
	if err != nil {
		return nil, err
	}
	if !current.IsAdmin() {
		return nil, ErrForbidden
	}

	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.first_name, u.last_name, c.email_normalized, m.role
		FROM household_memberships m
		JOIN users u ON u.id = m.user_id
		JOIN auth_credentials c ON c.user_id = u.id
		WHERE m.household_id = $1
		ORDER BY lower(u.first_name), lower(u.last_name), c.email_normalized`,
		householdID,
	)
	if err != nil {
		return nil, fmt.Errorf("list household members: %w", err)
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var member Member
		var role string
		if err := rows.Scan(&member.UserID, &member.FirstName, &member.LastName, &member.Email, &role); err != nil {
			return nil, fmt.Errorf("scan household member: %w", err)
		}
		member.Role = Role(role)
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate household members: %w", err)
	}
	return members, nil
}

// GetOptions returns household defaults for a member.
func (s *Service) GetOptions(ctx context.Context, userID, householdID uuid.UUID) (Options, error) {
	var options Options
	err := s.pool.QueryRow(ctx, `
		SELECT o.default_income_currency_code, o.default_spending_currency_code,
		       o.default_display_currency_code
		FROM household_options o
		JOIN household_memberships m ON m.household_id = o.household_id
		WHERE o.household_id = $1 AND m.user_id = $2`,
		householdID, userID,
	).Scan(
		&options.DefaultIncomeCurrencyCode,
		&options.DefaultSpendingCurrencyCode,
		&options.DefaultDisplayCurrencyCode,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Options{}, ErrNotFound
	}
	if err != nil {
		return Options{}, fmt.Errorf("get household options: %w", err)
	}
	return options, nil
}

func scanHousehold(row pgx.Row) (Household, error) {
	var household Household
	var role string
	err := row.Scan(&household.ID, &household.Name, &role, &household.CreatedAt, &household.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Household{}, ErrNotFound
	}
	if err != nil {
		return Household{}, fmt.Errorf("scan household: %w", err)
	}
	household.Role = Role(role)
	return household, nil
}

func validateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	switch {
	case name == "":
		return "", &ValidationError{Field: "name", Message: "Household name is required."}
	case !utf8.ValidString(name):
		return "", &ValidationError{Field: "name", Message: "Household name contains invalid characters."}
	case utf8.RuneCountInString(name) > 100:
		return "", &ValidationError{Field: "name", Message: "Household name must be 100 characters or fewer."}
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return "", &ValidationError{Field: "name", Message: "Household name contains invalid characters."}
	}
	return name, nil
}
