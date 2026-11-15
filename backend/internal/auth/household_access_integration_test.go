//go:build integration

package auth_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
	"budgettracker/internal/testdb"
)

func TestSetActiveHousehold(t *testing.T) {
	pool := testdb.New(t)
	service := auth.NewService(pool)
	householdService := households.NewService(pool)
	ctx := context.Background()

	session, err := service.Register(ctx, auth.RegisterParams{
		FirstName: "Ada",
		LastName:  "Lovelace",
		Email:     "ada@example.com",
		Password:  "correct horse battery staple",
	})
	require.NoError(t, err, "Register() error")
	home, err := householdService.CreateAndActivate(ctx, session.User.ID, session.TokenHash, "Home")
	require.NoError(t, err, "CreateAndActivate household")

	require.NoError(t, service.SetActiveHousehold(ctx, session.TokenHash, session.User.ID, nil), "SetActiveHousehold(nil)")
	reloaded, err := service.SessionByToken(ctx, session.Token)
	require.NoError(t, err, "SessionByToken() error")
	assert.Nil(t, reloaded.ActiveHouseholdID, "active household")

	require.NoError(t, service.SetActiveHousehold(ctx, session.TokenHash, session.User.ID, &home.ID), "SetActiveHousehold()")
	reloaded, err = service.SessionByToken(ctx, session.Token)
	require.NoError(t, err, "SessionByToken() error")
	require.NotNil(t, reloaded.ActiveHouseholdID, "active household")
	assert.Equal(t, home.ID, *reloaded.ActiveHouseholdID, "active household")

	otherSession, err := service.Register(ctx, auth.RegisterParams{
		FirstName: "Grace",
		LastName:  "Hopper",
		Email:     "grace@example.com",
		Password:  "correct horse battery staple",
	})
	require.NoError(t, err, "Register other user")
	otherHome, err := householdService.CreateAndActivate(ctx, otherSession.User.ID, otherSession.TokenHash, "Other home")
	require.NoError(t, err, "CreateAndActivate other household")

	err = service.SetActiveHousehold(ctx, session.TokenHash, session.User.ID, &otherHome.ID)
	assert.ErrorIs(t, err, auth.ErrHouseholdAccess, "SetActiveHousehold() other household")

	err = service.SetActiveHousehold(ctx, strings.Repeat("ab", 32), session.User.ID, &home.ID)
	assert.ErrorIs(t, err, auth.ErrSessionNotFound, "SetActiveHousehold() missing session")
}
