//go:build integration

package households

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	authService := auth.NewService(pool, nil)
	service := NewService(pool)
	ctx := context.Background()

	admin := registerInviteUser(t, authService, "admin@example.com")
	member := registerInviteUser(t, authService, "member@example.com")
	outsider := registerInviteUser(t, authService, "outsider@example.com")
	household, err := service.CreateAndActivate(ctx, admin.User.ID, admin.TokenHash, "Home")
	require.NoError(t, err, "create household")
	if _, err := pool.Exec(ctx, `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, household.ID, member.User.ID,
	); err != nil {
		require.NoError(t, err, "add member")
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
	require.NoError(t, err, "register %s", email)
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
	require.NoError(t, err, "CreateInvitation() error")
	assert.NotEmpty(t, rawToken, "invitation = %+v", invitation)
	assert.Equal(t, "new@example.com", invitation.Email, "invitation = %+v", invitation)
	assert.Equal(t, RoleMember, invitation.Role, "invitation = %+v", invitation)
	assert.GreaterOrEqual(t, time.Until(invitation.ExpiresAt), 6*24*time.Hour, "expiry")

	var storedHash string
	if err := fixture.pool.QueryRow(ctx,
		"SELECT token_hash FROM household_invitations WHERE id = $1", invitation.ID,
	).Scan(&storedHash); err != nil {
		require.NoError(t, err, "read token hash")
	}
	assert.NotEqual(t, rawToken, storedHash, "stored token hash")
	assert.Len(t, storedHash, 64, "stored token hash")

	_, _, err = fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
		Email: "new@example.com", Role: RoleMember,
	})
	assert.ErrorIs(t, err, ErrInvitationExists, "duplicate invitation")

	_, _, err = fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.member.User.ID,
		Email: "other@example.com", Role: RoleMember,
	})
	assert.ErrorIs(t, err, ErrForbidden, "member invite")
	_, _, err = fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.outsider.User.ID,
		Email: "other@example.com", Role: RoleMember,
	})
	assert.ErrorIs(t, err, ErrNotFound, "outsider invite")
	_, _, err = fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
		Email: "member@example.com", Role: RoleMember,
	})
	assert.ErrorIs(t, err, ErrAlreadyMember, "existing member invite")
	_, _, err = fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
		Email: "not-an-email", Role: RoleMember,
	})
	assert.Error(t, err, "invalid email invitation")

	invitations, err := fixture.service.ListInvitations(ctx, fixture.admin.User.ID, fixture.household.ID)
	require.NoError(t, err, "ListInvitations() error")
	require.Len(t, invitations, 1, "invitations")
	assert.Equal(t, invitation.ID, invitations[0].ID, "invitations")

	_, err = fixture.service.ListInvitations(ctx, fixture.member.User.ID, fixture.household.ID)
	assert.ErrorIs(t, err, ErrForbidden, "member ListInvitations()")

	err = fixture.service.RevokeInvitation(ctx, fixture.member.User.ID, fixture.household.ID, invitation.ID)
	assert.ErrorIs(t, err, ErrForbidden, "member RevokeInvitation()")

	require.NoError(t, fixture.service.RevokeInvitation(ctx, fixture.admin.User.ID, fixture.household.ID, invitation.ID), "RevokeInvitation()")

	_, err = fixture.service.InvitationByToken(ctx, rawToken)
	assert.ErrorIs(t, err, ErrInvitationNotFound, "revoked token")
}

func TestInvitationAcceptance(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()

	invitation, rawToken, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
		Email: "invited@example.com", Role: RoleAdmin,
	})
	require.NoError(t, err, "CreateInvitation() error")
	invited := registerInviteUser(t, fixture.auth, "invited@example.com")

	lookedUp, err := fixture.service.InvitationByToken(ctx, rawToken)
	require.NoError(t, err, "InvitationByToken() error")
	assert.Equal(t, "Home", lookedUp.HouseholdName, "looked up invitation = %+v", lookedUp)
	assert.Nil(t, lookedUp.AcceptedAt, "looked up invitation = %+v", lookedUp)
	assert.True(t, time.Now().Before(lookedUp.ExpiresAt), "new invitation is already expired")

	accepted, err := fixture.service.AcceptInvitation(ctx, rawToken, invited.User.ID)
	require.NoError(t, err, "AcceptInvitation() error")
	assert.Equal(t, invitation.HouseholdID, accepted.HouseholdID, "accepted invitation = %+v", accepted)
	assert.NotNil(t, accepted.AcceptedAt, "accepted invitation = %+v", accepted)
	role, err := fixture.service.membershipRole(ctx, fixture.household.ID, invited.User.ID)
	require.NoError(t, err, "look up accepted role")
	assert.Equal(t, RoleAdmin, role, "accepted role")

	_, err = fixture.service.AcceptInvitation(ctx, rawToken, invited.User.ID)
	assert.ErrorIs(t, err, ErrInvitationAccepted, "second accept")

	err = fixture.service.RevokeInvitation(ctx, fixture.admin.User.ID, fixture.household.ID, invitation.ID)
	assert.ErrorIs(t, err, ErrInvitationAccepted, "revoke accepted")
}

func TestInvitationEmailExpiryAndMismatch(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()

	_, rawToken, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
		Email: "invited@example.com", Role: RoleMember,
	})
	require.NoError(t, err, "CreateInvitation() error")
	mismatch := registerInviteUser(t, fixture.auth, "mismatch@example.com")
	_, err = fixture.service.AcceptInvitation(ctx, rawToken, mismatch.User.ID)
	assert.ErrorIs(t, err, ErrInvitationEmailMismatch, "mismatch accept")

	_, err = fixture.service.AcceptInvitation(ctx, "not-a-token", mismatch.User.ID)
	assert.ErrorIs(t, err, ErrInvitationNotFound, "invalid token")

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE household_invitations
		SET expires_at = now() - interval '1 minute'
		WHERE token_hash = $1`, hashInvitationToken(rawToken),
	); err != nil {
		require.NoError(t, err, "expire invitation")
	}
	invited := registerInviteUser(t, fixture.auth, "invited@example.com")
	_, err = fixture.service.AcceptInvitation(ctx, rawToken, invited.User.ID)
	assert.ErrorIs(t, err, ErrInvitationExpired, "expired accept")
}

func TestInvitationCrossHousehold(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()
	other, err := fixture.service.CreateAndActivate(ctx, fixture.outsider.User.ID, fixture.outsider.TokenHash, "Other")
	require.NoError(t, err, "create other household")
	invitation, _, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: other.ID, ActorID: fixture.outsider.User.ID,
		Email: "someone@example.com", Role: RoleMember,
	})
	require.NoError(t, err, "create other invitation")
	err = fixture.service.RevokeInvitation(ctx, fixture.admin.User.ID, fixture.household.ID, invitation.ID)
	assert.ErrorIs(t, err, ErrInvitationNotFound, "mismatched revoke")

	_, err = fixture.service.ListInvitations(ctx, fixture.admin.User.ID, other.ID)
	assert.ErrorIs(t, err, ErrNotFound, "cross-household list")
}

func TestInvitationRevocationAndAcceptanceOrderings(t *testing.T) {
	t.Run("accepted invitations cannot be revoked", func(t *testing.T) {
		fixture := newInvitationFixture(t)
		ctx := context.Background()
		invitation, rawToken, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
			HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
			Email: "accepted@example.com", Role: RoleMember,
		})
		require.NoError(t, err, "CreateInvitation() error")
		invited := registerInviteUser(t, fixture.auth, "accepted@example.com")
		if _, err := fixture.service.AcceptInvitation(ctx, rawToken, invited.User.ID); err != nil {
			require.NoError(t, err, "AcceptInvitation()")
		}
		err = fixture.service.RevokeInvitation(ctx, fixture.admin.User.ID, fixture.household.ID, invitation.ID)
		assert.ErrorIs(t, err, ErrInvitationAccepted, "revoke accepted")
		var acceptedAt *time.Time
		if err := fixture.pool.QueryRow(ctx,
			"SELECT accepted_at FROM household_invitations WHERE id = $1", invitation.ID,
		).Scan(&acceptedAt); err != nil {
			require.NoError(t, err, "accepted invitation disappeared")
		}
		assert.NotNil(t, acceptedAt, "accepted invitation lost its accepted timestamp")
	})

	t.Run("revocation prevents membership creation", func(t *testing.T) {
		fixture := newInvitationFixture(t)
		ctx := context.Background()
		invitation, rawToken, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
			HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
			Email: "revoked@example.com", Role: RoleMember,
		})
		require.NoError(t, err, "CreateInvitation() error")
		if err := fixture.service.RevokeInvitation(ctx, fixture.admin.User.ID, fixture.household.ID, invitation.ID); err != nil {
			require.NoError(t, err, "RevokeInvitation()")
		}
		invited := registerInviteUser(t, fixture.auth, "revoked@example.com")
		_, err = fixture.service.AcceptInvitation(ctx, rawToken, invited.User.ID)
		assert.ErrorIs(t, err, ErrInvitationNotFound, "accept revoked")
		var memberships int
		if err := fixture.pool.QueryRow(ctx,
			"SELECT count(*) FROM household_memberships WHERE household_id = $1 AND user_id = $2",
			fixture.household.ID, invited.User.ID,
		).Scan(&memberships); err != nil {
			require.NoError(t, err, "count memberships")
		}
		assert.Zero(t, memberships, "revoked invitation created a membership")
	})
}

func TestInvitationConcurrentAcceptAndRevoke(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()
	invitation, rawToken, err := fixture.service.CreateInvitation(ctx, CreateInvitationParams{
		HouseholdID: fixture.household.ID, ActorID: fixture.admin.User.ID,
		Email: "race@example.com", Role: RoleMember,
	})
	require.NoError(t, err, "CreateInvitation() error")
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
		require.NoError(t, err, "count memberships")
	}

	switch {
	case acceptErr == nil && revokeErr == nil:
		require.Fail(t, "acceptance and revocation both succeeded")
	case acceptErr == nil:
		assert.ErrorIs(t, revokeErr, ErrInvitationAccepted, "revoke after acceptance")
		assert.Equal(t, 1, memberships, "memberships after acceptance")
	case revokeErr == nil:
		assert.ErrorIs(t, acceptErr, ErrInvitationNotFound, "accept after revocation")
		assert.Zero(t, memberships, "memberships after revocation")
	default:
		require.Fail(t, "both operations failed", "accept %v, revoke %v", acceptErr, revokeErr)
	}
}
