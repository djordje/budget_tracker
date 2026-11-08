//go:build integration

package households

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

func TestHouseholdOptionsAndUserInheritance(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()

	effective, err := fixture.service.EffectiveOptions(ctx, fixture.member.User.ID, fixture.household.ID)
	if err != nil {
		t.Fatalf("EffectiveOptions() error = %v", err)
	}
	if effective.DefaultIncomeCurrencyCode != "RSD" || effective.DefaultDisplayCurrencyCode != "RSD" {
		t.Errorf("default effective options = %+v", effective)
	}

	income := "EUR"
	view, err := fixture.service.SetUserOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode: &income,
	})
	if err != nil {
		t.Fatalf("SetUserOptions() error = %v", err)
	}
	if view.User.DefaultIncomeCurrencyCode == nil || *view.User.DefaultIncomeCurrencyCode != "EUR" {
		t.Errorf("user options = %+v", view.User)
	}
	if view.Effective.DefaultIncomeCurrencyCode != "EUR" ||
		view.Effective.DefaultSpendingCurrencyCode != "RSD" ||
		view.Household.DefaultSpendingCurrencyCode != "RSD" {
		t.Errorf("update view = %+v, want committed overrides and inherited defaults", view)
	}

	stored, err := fixture.service.GetUserOptions(ctx, fixture.member.User.ID, fixture.household.ID)
	if err != nil {
		t.Fatalf("GetUserOptions() error = %v", err)
	}
	if stored.DefaultIncomeCurrencyCode == nil || *stored.DefaultIncomeCurrencyCode != "EUR" {
		t.Errorf("stored options = %+v", stored)
	}
	if stored.DefaultSpendingCurrencyCode != nil {
		t.Errorf("spending override = %v, want nil", stored.DefaultSpendingCurrencyCode)
	}

	effective, err = fixture.service.EffectiveOptions(ctx, fixture.member.User.ID, fixture.household.ID)
	if err != nil {
		t.Fatalf("EffectiveOptions() with override error = %v", err)
	}
	if effective.DefaultIncomeCurrencyCode != "EUR" || effective.DefaultSpendingCurrencyCode != "RSD" {
		t.Errorf("effective options = %+v, want income override and spending inherit", effective)
	}

	display := "USD"
	if _, err := fixture.service.SetUserOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode:   &income,
		DefaultSpendingCurrencyCode: &display,
		DefaultDisplayCurrencyCode:  &display,
	}); err != nil {
		t.Fatalf("SetUserOptions() full error = %v", err)
	}
	effective, _ = fixture.service.EffectiveOptions(ctx, fixture.member.User.ID, fixture.household.ID)
	if effective.DefaultDisplayCurrencyCode != "USD" || effective.DefaultSpendingCurrencyCode != "USD" {
		t.Errorf("full effective options = %+v", effective)
	}

	if _, err := fixture.service.SetUserOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode: ptr("XXX"),
	}); err == nil {
		t.Error("invalid override error = nil")
	} else {
		var validation *OptionsValidationError
		if !errors.As(err, &validation) || validation.Fields["default_income_currency_code"] == "" {
			t.Errorf("validation error = %v", err)
		}
	}

	empty := ""
	cleared, err := fixture.service.SetUserOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode: &empty,
	})
	if err != nil {
		t.Fatalf("clear override error = %v", err)
	}
	if cleared.User.DefaultIncomeCurrencyCode != nil {
		t.Errorf("cleared override = %v", cleared.User.DefaultIncomeCurrencyCode)
	}
	if cleared.Effective.DefaultIncomeCurrencyCode != "RSD" {
		t.Errorf("cleared effective income = %q, want inherited RSD", cleared.Effective.DefaultIncomeCurrencyCode)
	}

	if _, err := fixture.service.GetUserOptions(ctx, fixture.outsider.User.ID, fixture.household.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider GetUserOptions error = %v, want ErrNotFound", err)
	}
}

func TestHouseholdOptionsUpdateAuthorization(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()

	updated, err := fixture.service.UpdateOptions(ctx, fixture.admin.User.ID, fixture.household.ID, UpdateOptionsParams{
		DefaultIncomeCurrencyCode: "EUR", DefaultSpendingCurrencyCode: "USD", DefaultDisplayCurrencyCode: "EUR",
	})
	if err != nil {
		t.Fatalf("UpdateOptions() error = %v", err)
	}
	if updated.DefaultIncomeCurrencyCode != "EUR" || updated.DefaultDisplayCurrencyCode != "EUR" {
		t.Errorf("updated options = %+v", updated)
	}

	effective, err := fixture.service.EffectiveOptions(ctx, fixture.member.User.ID, fixture.household.ID)
	if err != nil {
		t.Fatalf("member EffectiveOptions() error = %v", err)
	}
	if effective.DefaultIncomeCurrencyCode != "EUR" || effective.DefaultSpendingCurrencyCode != "USD" {
		t.Errorf("member inherited options = %+v", effective)
	}

	if _, err := fixture.service.UpdateOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateOptionsParams{
		DefaultIncomeCurrencyCode: "RSD", DefaultSpendingCurrencyCode: "RSD", DefaultDisplayCurrencyCode: "RSD",
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("member UpdateOptions error = %v, want ErrForbidden", err)
	}
	if _, err := fixture.service.UpdateOptions(ctx, fixture.outsider.User.ID, fixture.household.ID, UpdateOptionsParams{
		DefaultIncomeCurrencyCode: "RSD", DefaultSpendingCurrencyCode: "RSD", DefaultDisplayCurrencyCode: "RSD",
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider UpdateOptions error = %v, want ErrNotFound", err)
	}
	if _, err := fixture.service.UpdateOptions(ctx, fixture.admin.User.ID, fixture.household.ID, UpdateOptionsParams{
		DefaultIncomeCurrencyCode: "XXX", DefaultSpendingCurrencyCode: "RSD", DefaultDisplayCurrencyCode: "RSD",
	}); err == nil {
		t.Error("invalid household currency error = nil")
	}
}

func ptr(value string) *string { return &value }

func TestOptionsViewIsConsistent(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()

	if _, err := fixture.service.UpdateOptions(ctx, fixture.admin.User.ID, fixture.household.ID, UpdateOptionsParams{
		DefaultIncomeCurrencyCode:   "EUR",
		DefaultSpendingCurrencyCode: "USD",
		DefaultDisplayCurrencyCode:  "EUR",
	}); err != nil {
		t.Fatalf("UpdateOptions() error = %v", err)
	}
	income := "GBP"
	view, err := fixture.service.SetUserOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode: &income,
	})
	if err != nil {
		t.Fatalf("SetUserOptions() error = %v", err)
	}

	if view.Household.DefaultIncomeCurrencyCode != "EUR" ||
		view.Household.DefaultSpendingCurrencyCode != "USD" {
		t.Errorf("household defaults = %+v", view.Household)
	}
	if view.User.DefaultIncomeCurrencyCode == nil || *view.User.DefaultIncomeCurrencyCode != "GBP" {
		t.Errorf("personal overrides = %+v", view.User)
	}
	if view.User.DefaultSpendingCurrencyCode != nil {
		t.Errorf("spending override = %v, want nil", view.User.DefaultSpendingCurrencyCode)
	}
	if view.Effective.DefaultIncomeCurrencyCode != "GBP" ||
		view.Effective.DefaultSpendingCurrencyCode != "USD" ||
		view.Effective.DefaultDisplayCurrencyCode != "EUR" {
		t.Errorf("effective options = %+v", view.Effective)
	}

	reloaded, err := fixture.service.GetOptionsView(ctx, fixture.member.User.ID, fixture.household.ID)
	if err != nil {
		t.Fatalf("GetOptionsView() error = %v", err)
	}
	if !reflect.DeepEqual(view, reloaded) {
		t.Errorf("update response = %+v, stored view = %+v", view, reloaded)
	}

	if _, err := fixture.service.GetOptionsView(ctx, fixture.outsider.User.ID, fixture.household.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("outsider GetOptionsView error = %v, want ErrNotFound", err)
	}
}

func TestOptionsViewConcurrentUpdatesStayConsistent(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		codes := []string{"EUR", "USD", "GBP"}
		for i := range 30 {
			code := codes[i%len(codes)]
			if _, err := fixture.service.SetUserOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateUserOptionsParams{
				DefaultIncomeCurrencyCode: &code,
			}); err != nil {
				t.Errorf("SetUserOptions() error = %v", err)
				return
			}
		}
	}()

	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 30 {
				view, err := fixture.service.GetOptionsView(ctx, fixture.member.User.ID, fixture.household.ID)
				if err != nil {
					t.Errorf("GetOptionsView() error = %v", err)
					return
				}
				assertEffective := func(user *string, household, effective string) {
					want := household
					if user != nil {
						want = *user
					}
					if effective != want {
						t.Errorf("effective = %q, want %q from user %v and household %q", effective, want, user, household)
					}
				}
				assertEffective(view.User.DefaultIncomeCurrencyCode, view.Household.DefaultIncomeCurrencyCode, view.Effective.DefaultIncomeCurrencyCode)
				assertEffective(view.User.DefaultSpendingCurrencyCode, view.Household.DefaultSpendingCurrencyCode, view.Effective.DefaultSpendingCurrencyCode)
				assertEffective(view.User.DefaultDisplayCurrencyCode, view.Household.DefaultDisplayCurrencyCode, view.Effective.DefaultDisplayCurrencyCode)
			}
		}()
	}
	wg.Wait()
}
