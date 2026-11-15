//go:build integration

package households

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHouseholdOptionsAndUserInheritance(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()

	first, err := fixture.service.GetOptionsView(ctx, fixture.member.User.ID, fixture.household.ID)
	require.NoError(t, err, "GetOptionsView() error")
	assert.Equal(t, "RSD", first.Effective.DefaultIncomeCurrencyCode, "default effective options = %+v", first)
	assert.Equal(t, "RSD", first.Effective.DefaultDisplayCurrencyCode, "default effective options = %+v", first)

	income := "EUR"
	view, err := fixture.service.SetUserOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode: &income,
	})
	require.NoError(t, err, "SetUserOptions() error")
	require.NotNil(t, view.User.DefaultIncomeCurrencyCode, "user options = %+v", view.User)
	assert.Equal(t, "EUR", *view.User.DefaultIncomeCurrencyCode, "user options = %+v", view.User)
	assert.Equal(t, "EUR", view.Effective.DefaultIncomeCurrencyCode, "update view = %+v", view)
	assert.Equal(t, "RSD", view.Effective.DefaultSpendingCurrencyCode, "update view = %+v", view)
	assert.Equal(t, "RSD", view.Household.DefaultSpendingCurrencyCode, "update view = %+v", view)

	stored, err := fixture.service.GetOptionsView(ctx, fixture.member.User.ID, fixture.household.ID)
	require.NoError(t, err, "GetOptionsView() error")
	require.NotNil(t, stored.User.DefaultIncomeCurrencyCode, "stored options = %+v", stored.User)
	assert.Equal(t, "EUR", *stored.User.DefaultIncomeCurrencyCode, "stored options = %+v", stored.User)
	assert.Nil(t, stored.User.DefaultSpendingCurrencyCode, "spending override")
	assert.Equal(t, "EUR", stored.Effective.DefaultIncomeCurrencyCode, "effective options = %+v", stored)
	assert.Equal(t, "RSD", stored.Effective.DefaultSpendingCurrencyCode, "effective options = %+v", stored)

	display := "USD"
	if _, err := fixture.service.SetUserOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode:   &income,
		DefaultSpendingCurrencyCode: &display,
		DefaultDisplayCurrencyCode:  &display,
	}); err != nil {
		require.NoError(t, err, "SetUserOptions() full")
	}
	stored, err = fixture.service.GetOptionsView(ctx, fixture.member.User.ID, fixture.household.ID)
	require.NoError(t, err, "GetOptionsView() error")
	assert.Equal(t, "USD", stored.Effective.DefaultDisplayCurrencyCode, "full effective options = %+v", stored)
	assert.Equal(t, "USD", stored.Effective.DefaultSpendingCurrencyCode, "full effective options = %+v", stored)

	_, err = fixture.service.SetUserOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode: ptr("XXX"),
	})
	require.Error(t, err, "invalid override")
	var validation *OptionsValidationError
	require.ErrorAs(t, err, &validation, "validation error")
	assert.NotEmpty(t, validation.Fields["default_income_currency_code"], "validation error = %v", err)

	empty := ""
	cleared, err := fixture.service.SetUserOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode: &empty,
	})
	require.NoError(t, err, "clear override error")
	assert.Nil(t, cleared.User.DefaultIncomeCurrencyCode, "cleared override")
	assert.Equal(t, "RSD", cleared.Effective.DefaultIncomeCurrencyCode, "cleared effective income")

	_, err = fixture.service.GetOptionsView(ctx, fixture.outsider.User.ID, fixture.household.ID)
	assert.ErrorIs(t, err, ErrNotFound, "outsider GetOptionsView()")
}

func TestHouseholdOptionsUpdateAuthorization(t *testing.T) {
	fixture := newInvitationFixture(t)
	ctx := context.Background()

	updated, err := fixture.service.UpdateOptions(ctx, fixture.admin.User.ID, fixture.household.ID, UpdateOptionsParams{
		DefaultIncomeCurrencyCode: "EUR", DefaultSpendingCurrencyCode: "USD", DefaultDisplayCurrencyCode: "EUR",
	})
	require.NoError(t, err, "UpdateOptions() error")
	assert.Equal(t, "EUR", updated.DefaultIncomeCurrencyCode, "updated options = %+v", updated)
	assert.Equal(t, "EUR", updated.DefaultDisplayCurrencyCode, "updated options = %+v", updated)

	view, err := fixture.service.GetOptionsView(ctx, fixture.member.User.ID, fixture.household.ID)
	require.NoError(t, err, "member GetOptionsView() error")
	assert.Equal(t, "EUR", view.Effective.DefaultIncomeCurrencyCode, "member inherited options = %+v", view)
	assert.Equal(t, "USD", view.Effective.DefaultSpendingCurrencyCode, "member inherited options = %+v", view)

	_, err = fixture.service.UpdateOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateOptionsParams{
		DefaultIncomeCurrencyCode: "RSD", DefaultSpendingCurrencyCode: "RSD", DefaultDisplayCurrencyCode: "RSD",
	})
	assert.ErrorIs(t, err, ErrForbidden, "member UpdateOptions()")
	_, err = fixture.service.UpdateOptions(ctx, fixture.outsider.User.ID, fixture.household.ID, UpdateOptionsParams{
		DefaultIncomeCurrencyCode: "RSD", DefaultSpendingCurrencyCode: "RSD", DefaultDisplayCurrencyCode: "RSD",
	})
	assert.ErrorIs(t, err, ErrNotFound, "outsider UpdateOptions()")
	_, err = fixture.service.UpdateOptions(ctx, fixture.admin.User.ID, fixture.household.ID, UpdateOptionsParams{
		DefaultIncomeCurrencyCode: "XXX", DefaultSpendingCurrencyCode: "RSD", DefaultDisplayCurrencyCode: "RSD",
	})
	assert.Error(t, err, "invalid household currency")
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
		require.NoError(t, err, "UpdateOptions()")
	}
	income := "GBP"
	view, err := fixture.service.SetUserOptions(ctx, fixture.member.User.ID, fixture.household.ID, UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode: &income,
	})
	require.NoError(t, err, "SetUserOptions() error")

	assert.Equal(t, "EUR", view.Household.DefaultIncomeCurrencyCode, "household defaults = %+v", view.Household)
	assert.Equal(t, "USD", view.Household.DefaultSpendingCurrencyCode, "household defaults = %+v", view.Household)
	require.NotNil(t, view.User.DefaultIncomeCurrencyCode, "personal overrides = %+v", view.User)
	assert.Equal(t, "GBP", *view.User.DefaultIncomeCurrencyCode, "personal overrides = %+v", view.User)
	assert.Nil(t, view.User.DefaultSpendingCurrencyCode, "spending override")
	assert.Equal(t, "GBP", view.Effective.DefaultIncomeCurrencyCode, "effective options = %+v", view.Effective)
	assert.Equal(t, "USD", view.Effective.DefaultSpendingCurrencyCode, "effective options = %+v", view.Effective)
	assert.Equal(t, "EUR", view.Effective.DefaultDisplayCurrencyCode, "effective options = %+v", view.Effective)

	reloaded, err := fixture.service.GetOptionsView(ctx, fixture.member.User.ID, fixture.household.ID)
	require.NoError(t, err, "GetOptionsView() error")
	assert.Equal(t, view, reloaded, "reloaded view")

	_, err = fixture.service.GetOptionsView(ctx, fixture.outsider.User.ID, fixture.household.ID)
	assert.ErrorIs(t, err, ErrNotFound, "outsider GetOptionsView()")
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
				assert.NoError(t, err, "SetUserOptions()")
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
					assert.NoError(t, err, "GetOptionsView()")
					return
				}
				assertEffective := func(user *string, household, effective string) {
					want := household
					if user != nil {
						want = *user
					}
					assert.Equal(t, want, effective, "effective from user %v and household %q", user, household)
				}
				assertEffective(view.User.DefaultIncomeCurrencyCode, view.Household.DefaultIncomeCurrencyCode, view.Effective.DefaultIncomeCurrencyCode)
				assertEffective(view.User.DefaultSpendingCurrencyCode, view.Household.DefaultSpendingCurrencyCode, view.Effective.DefaultSpendingCurrencyCode)
				assertEffective(view.User.DefaultDisplayCurrencyCode, view.Household.DefaultDisplayCurrencyCode, view.Effective.DefaultDisplayCurrencyCode)
			}
		}()
	}
	wg.Wait()
}
