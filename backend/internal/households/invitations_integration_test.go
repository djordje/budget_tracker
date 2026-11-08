//go:build integration

package households

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"budgettracker/internal/auth"
	"budgettracker/internal/testdb"
)

type invitationFixture struct {
	pool      *pgxpool.Pool
	service   *Service
	auth      *auth.Service
	household Household
	admin     auth.Session
	member    auth.Session
	outsider  auth.Session
}

func newInvitationFixture(t *testing.T) *invitationFixture {
	t.Helper()
	pool := testdb.New(t)
	authService := auth.NewService(pool)
	service := NewService(pool)
	ctx := context.Background()

	admin := registerInviteUser(t, authService, "admin@example.com")
	member := registerInviteUser(t, authService, "member@example.com")
	outsider := registerInviteUser(t, authService, "outsider@example.com")
	household, err := service.CreateAndActivate(ctx, admin.User.ID, admin.TokenHash, "Home")
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, household.ID, member.User.ID,
	); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return &invitationFixture{
		pool: pool, service: service, auth: authService,
		household: household, admin: admin, member: member, outsider: outsider,
	}
}

func registerInviteUser(t *testing.T, service *auth.Service, email string) auth.Session {
	t.Helper()
	session, err := service.Register(context.Background(), auth.RegisterParams{
		FirstName: "Test", LastName: "User", Email: email,
		Password: "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	return session
}

func TestInvitationCreateListRevoke(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()

	invitation, rawToken, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID,
		ActorID:     fixture.admin.User.ID,
		Email:       " New@Example.com ",
		Role:        RoleMember,
	})
	if err != nil {
		t.Fatalf("CreateInvitation() error = %v", err)
	}
	if rawToken == "" || invitation.Email != "new@example.com" || invitation.Role != RoleMember {
		t.Errorf("invitation = %+v, token = %q", invitation, rawToken)
	}
	if time.Until(invitation.ExpiresAt) < 6*24*time.Hour {
		t.Errorf("expiry = %v, want about 7 days", invitation.ExpiresAt)
	}

	var storedHash string
	if err := fixture.pool.QueryRow(ctx,
		"SELECT token_hash FROM household_invitations WHERE id = $1", invitation.ID,
	).Scan(&storedHash); err != nil {
		t.Fatalf("read token hash: %v", err)
	}
	if storedHash == rawToken || len(storedHash) != 64 {
		t.Errorf("stored token hash = %q", storedHash)
	}

	if _, _, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
		Email: "new@example.com", Role: RoleMember,
	}); !errors.Is(err, ErrInvitationExists) {
		t.Errorf("duplicate invitation error = %v, want ErrInvitationExists", err)
	}

	if _, _, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.member.User.ID,
		Email: "other@example.com", Role: RoleMember,
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("member invite error = %v, want ErrForbidden", err)
	}
	if _, _, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.outsider.User.ID,
		Email: "other@example.com", Role: RoleMember,
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider invite error = %v, want ErrNotFound", err)
	}
	if _, _, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
		Email: "member@example.com", Role: RoleMember,
	}); !errors.Is(err, ErrAlreadyMember) {
		t.Errorf("existing member invite error = %v, want ErrAlreadyMember", err)
	}
	if _, _, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
		Email: "not-an-email", Role: RoleMember,
	}); err == nil {
		t.Error("invalid email invitation error = nil")
	}

	invitations, err := fixture.service.ListInvitations(ctx, fixture.admin.User.ID, fixture.household.ID)
	if err != nil {
		t.Fatalf("ListInvitations() error = %v", err)
	}
	if len(invitations) != 1 || invitations[0].ID != invitation.ID {
		t.Errorf("invitations = %+v", invitations)
	}
	if _, err := fixture.service.ListInvitations(ctx, fixture.member.User.ID, fixture.household.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("member list invitations error = %v, want ErrForbidden", err)
	}

	if err := fixture.service.RevokeInvitation(ctx, fixture.member.User.ID, fixture.household.ID, invitation.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("member revoke error = %v, want ErrForbidden", err)
	}
	if err := fixture.service.RevokeInvitation(ctx, fixture.admin.User.ID, fixture.household.ID, invitation.ID); err != nil {
		t.Fatalf("RevokeInvitation() error = %v", err)
	}
	if _, err := fixture.service.InvitationByToken(ctx, rawToken); !errors.Is(err, ErrInvitationNotFound) {
		t.Errorf("revoked token error = %v, want ErrInvitationNotFound", err)
	}
}

func TestInvitationAcceptance(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()

	invitation, rawToken, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
		Email: "invited@example.com", Role: RoleAdmin,
	})
	if err != nil {
		t.Fatalf("CreateInvitation() error = %v", err)
	}
	invited := registerInviteUser(t, fixture.auth, "invited@example.com")

	lookedUp, err := fixture.service.InvitationByToken(ctx, rawToken)
	if err != nil {
		t.Fatalf("InvitationByToken() error = %v", err)
	}
	if lookedUp.HouseholdName != "Home" || lookedUp.AcceptedAt != nil {
		t.Errorf("looked up invitation = %+v", lookedUp)
	}
	if !time.Now().Before(lookedUp.ExpiresAt) {
		t.Error("new invitation is already expired")
	}

	accepted, err := fixture.service.AcceptInvitation(ctx, rawToken, invited.User.ID)
	if err != nil {
		t.Fatalf("AcceptInvitation() error = %v", err)
	}
	if accepted.HouseholdID != invitation.HouseholdID || accepted.AcceptedAt == nil {
		t.Errorf("accepted invitation = %+v", accepted)
	}
	role, err := fixture.service.membershipRole(ctx, fixture.household.ID, invited.User.ID)
	if err != nil {
		t.Fatalf("look up accepted role: %v", err)
	}
	if role != RoleAdmin {
		t.Errorf("accepted role = %q, want admin", role)
	}

	if _, err := fixture.service.AcceptInvitation(ctx, rawToken, invited.User.ID); !errors.Is(err, ErrInvitationAccepted) {
		t.Errorf("second accept error = %v, want ErrInvitationAccepted", err)
	}
	if err := fixture.service.RevokeInvitation(ctx, fixture.admin.User.ID, fixture.household.ID, invitation.ID); !errors.Is(err, ErrInvitationAccepted) {
		t.Errorf("revoke accepted error = %v, want ErrInvitationAccepted", err)
	}
}

func TestInvitationEmailExpiryAndMismatch(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()

	_, rawToken, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
		Email: "invited@example.com", Role: RoleMember,
	})
	if err != nil {
		t.Fatalf("CreateInvitation() error = %v", err)
	}
	mismatch := registerInviteUser(t, fixture.auth, "mismatch@example.com")
	if _, err := fixture.service.AcceptInvitation(ctx, rawToken, mismatch.User.ID); !errors.Is(err, ErrInvitationEmailMismatch) {
		t.Errorf("mismatch accept error = %v, want ErrInvitationEmailMismatch", err)
	}
	if _, err := fixture.service.AcceptInvitation(ctx, "not-a-token", mismatch.User.ID); !errors.Is(err, ErrInvitationNotFound) {
		t.Errorf("invalid token error = %v, want ErrInvitationNotFound", err)
	}

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE household_invitations
		SET expires_at = now() - interval '1 minute'
		WHERE token_hash = $1`, hashInvitationToken(rawToken),
	); err != nil {
		t.Fatalf("expire invitation: %v", err)
	}
	invited := registerInviteUser(t, fixture.auth, "invited@example.com")
	if _, err := fixture.service.AcceptInvitation(ctx, rawToken, invited.User.ID); !errors.Is(err, ErrInvitationExpired) {
		t.Errorf("expired accept error = %v, want ErrInvitationExpired", err)
	}
}

func TestInvitationCrossHousehold(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()
	other, err := fixture.service.CreateAndActivate(ctx, fixture.outsider.User.ID, fixture.outsider.TokenHash, "Other")
	if err != nil {
		t.Fatalf("create other household: %v", err)
	}
	invitation, _, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: other.ID, ActorID: fixture.outsider.User.ID,
		Email: "someone@example.com", Role: RoleMember,
	})
	if err != nil {
		t.Fatalf("create other invitation: %v", err)
	}
	if err := fixture.service.RevokeInvitation(ctx, fixture.admin.User.ID, fixture.household.ID, invitation.ID); !errors.Is(err, ErrInvitationNotFound) {
		t.Errorf("mismatched revoke error = %v, want ErrInvitationNotFound", err)
	}
	if _, err := fixture.service.ListInvitations(ctx, fixture.admin.User.ID, other.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-household list error = %v, want ErrNotFound", err)
	}
}

func TestInvitationRevocationAndAcceptanceOrderings(t *testing.T) {
	t.Run("accepted invitations cannot be revoked", func(t *testing.T) {
		fixture := newInvitationFixture(t)
		ctx := context.Background()
		invitation, rawToken, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
			HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
			Email: "accepted@example.com", Role: RoleMember,
		})
		if err != nil {
			t.Fatalf("CreateInvitation() error = %v", err)
		}
		invited := registerInviteUser(t, fixture.auth, "accepted@example.com")
		if _, err := fixture.service.AcceptInvitation(ctx, rawToken, invited.User.ID); err != nil {
			t.Fatalf("AcceptInvitation() error = %v", err)
		}
		if err := fixture.service.RevokeInvitation(ctx, fixture.admin.User.ID, fixture.household.ID, invitation.ID); !errors.Is(err, ErrInvitationAccepted) {
			t.Errorf("revoke accepted error = %v, want ErrInvitationAccepted", err)
		}
		var acceptedAt *time.Time
		if err := fixture.pool.QueryRow(ctx,
			"SELECT accepted_at FROM household_invitations WHERE id = $1", invitation.ID,
		).Scan(&acceptedAt); err != nil {
			t.Fatalf("accepted invitation disappeared: %v", err)
		}
		if acceptedAt == nil {
			t.Error("accepted invitation lost its accepted timestamp")
		}
	})

	t.Run("revocation prevents membership creation", func(t *testing.T) {
		fixture := newInvitationFixture(t)
		ctx := context.Background()
		invitation, rawToken, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
			HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
			Email: "revoked@example.com", Role: RoleMember,
		})
		if err != nil {
			t.Fatalf("CreateInvitation() error = %v", err)
		}
		if err := fixture.service.RevokeInvitation(ctx, fixture.admin.User.ID, fixture.household.ID, invitation.ID); err != nil {
			t.Fatalf("RevokeInvitation() error = %v", err)
		}
		invited := registerInviteUser(t, fixture.auth, "revoked@example.com")
		if _, err := fixture.service.AcceptInvitation(ctx, rawToken, invited.User.ID); !errors.Is(err, ErrInvitationNotFound) {
			t.Errorf("accept revoked error = %v, want ErrInvitationNotFound", err)
		}
		var memberships int
		if err := fixture.pool.QueryRow(ctx,
			"SELECT count(*) FROM household_memberships WHERE household_id = $1 AND user_id = $2",
			fixture.household.ID, invited.User.ID,
		).Scan(&memberships); err != nil {
			t.Fatalf("count memberships: %v", err)
		}
		if memberships != 0 {
			t.Error("revoked invitation created a membership")
		}
	})
}

func TestInvitationConcurrentAcceptAndRevoke(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()
	invitation, rawToken, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
		Email: "race@example.com", Role: RoleMember,
	})
	if err != nil {
		t.Fatalf("CreateInvitation() error = %v", err)
	}
	invited := registerInviteUser(t, fixture.auth, "race@example.com")

	start := make(chan struct{})
	var acceptErr, revokeErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, acceptErr = fixture.service.AcceptInvitation(ctx, rawToken, invited.User.ID)
	}()
	go func() {
		defer wg.Done()
		<-start
		revokeErr = fixture.service.RevokeInvitation(ctx, fixture.admin.User.ID, fixture.household.ID, invitation.ID)
	}()
	close(start)
	wg.Wait()

	var memberships int
	if err := fixture.pool.QueryRow(ctx,
		"SELECT count(*) FROM household_memberships WHERE household_id = $1 AND user_id = $2",
		fixture.household.ID, invited.User.ID,
	).Scan(&memberships); err != nil {
		t.Fatalf("count memberships: %v", err)
	}

	switch {
	case acceptErr == nil && revokeErr == nil:
		t.Fatal("acceptance and revocation both succeeded")
	case acceptErr == nil:
		if !errors.Is(revokeErr, ErrInvitationAccepted) {
			t.Errorf("revoke after acceptance error = %v, want ErrInvitationAccepted", revokeErr)
		}
		if memberships != 1 {
			t.Errorf("memberships = %d, want 1 after acceptance", memberships)
		}
	case revokeErr == nil:
		if !errors.Is(acceptErr, ErrInvitationNotFound) {
			t.Errorf("accept after revocation error = %v, want ErrInvitationNotFound", acceptErr)
		}
		if memberships != 0 {
			t.Errorf("memberships = %d, want 0 after revocation", memberships)
		}
	default:
		t.Fatalf("both operations failed: accept %v, revoke %v", acceptErr, revokeErr)
	}
}
