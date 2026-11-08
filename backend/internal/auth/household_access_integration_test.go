//go:build integration

package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	home, err := householdService.CreateAndActivate(ctx, session.User.ID, session.TokenHash, "Home")
	if err != nil {
		t.Fatalf("CreateAndActivate household: %v", err)
	}

	if err := service.SetActiveHousehold(ctx, session.TokenHash, session.User.ID, nil); err != nil {
		t.Fatalf("SetActiveHousehold(nil) error = %v", err)
	}
	reloaded, err := service.SessionByToken(ctx, session.Token)
	if err != nil {
		t.Fatalf("SessionByToken() error = %v", err)
	}
	if reloaded.ActiveHouseholdID != nil {
		t.Errorf("active household = %v, want nil", reloaded.ActiveHouseholdID)
	}

	if err := service.SetActiveHousehold(ctx, session.TokenHash, session.User.ID, &home.ID); err != nil {
		t.Fatalf("SetActiveHousehold() error = %v", err)
	}
	reloaded, err = service.SessionByToken(ctx, session.Token)
	if err != nil {
		t.Fatalf("SessionByToken() error = %v", err)
	}
	if reloaded.ActiveHouseholdID == nil || *reloaded.ActiveHouseholdID != home.ID {
		t.Errorf("active household = %v, want %v", reloaded.ActiveHouseholdID, home.ID)
	}

	otherSession, err := service.Register(ctx, auth.RegisterParams{
		FirstName: "Grace",
		LastName:  "Hopper",
		Email:     "grace@example.com",
		Password:  "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("Register other user: %v", err)
	}
	otherHome, err := householdService.CreateAndActivate(ctx, otherSession.User.ID, otherSession.TokenHash, "Other home")
	if err != nil {
		t.Fatalf("CreateAndActivate other household: %v", err)
	}

	if err := service.SetActiveHousehold(ctx, session.TokenHash, session.User.ID, &otherHome.ID); !errors.Is(err, auth.ErrHouseholdAccess) {
		t.Errorf("SetActiveHousehold() other household error = %v, want ErrHouseholdAccess", err)
	}
	if err := service.SetActiveHousehold(ctx, strings.Repeat("ab", 32), session.User.ID, &home.ID); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Errorf("SetActiveHousehold() missing session error = %v, want ErrSessionNotFound", err)
	}
}
