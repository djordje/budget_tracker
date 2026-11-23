package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/households"
)

// failingRateService returns a fixed service error from rate creation and
// delegates the remaining operations to stubRates.
type failingRateService struct {
	stubRates
	createErr error
}

func (s failingRateService) CreateExchangeRate(
	context.Context, finance.CreateExchangeRateParams,
) (finance.ExchangeRate, error) {
	return finance.ExchangeRate{}, s.createErr
}

func TestRateFormRendersValidationCopy(t *testing.T) {
	userID := uuid.New()
	householdID := uuid.New()
	handler, err := New(Deps{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions: stubSessions{},
		Households: stubHouseholds{
			households: []households.Household{{ID: householdID, Name: "Home", Role: households.RoleAdmin}},
		},
		Entries: stubEntries{},
		Rates: failingRateService{
			createErr: &finance.ValidationError{Fields: map[string]string{"rate": "Enter a smaller rate."}},
		},
		Reports: stubReports{},
	})
	require.NoError(t, err, "New()")

	session := auth.Session{
		UserID:            userID,
		User:              auth.User{ID: userID},
		ActiveHouseholdID: &householdID,
	}
	form := url.Values{
		"from_currency_code": {"EUR"},
		"to_currency_code":   {"RSD"},
		"rate":               {"1000000000000"},
		"rate_date":          {"2026-10-15"},
	}
	request := httptest.NewRequest(http.MethodPost, householdURL(householdID, "/exchange-rates"), strings.NewReader(form.Encode()))
	request.SetPathValue("householdID", householdID.String())
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
	recorder := httptest.NewRecorder()
	auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil))).
		Load(handler.Scoped("more", "", handler.CreateExchangeRate)).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, "body = %s", recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "Enter a smaller rate.", "rendered validation copy")
}

func TestRateFormDefaultsToUTCDate(t *testing.T) {
	before := finance.Today()
	recorder := serveHouseholdPage(t, func(h *Handler) http.HandlerFunc { return h.ExchangeRatesPage })
	// A request can straddle UTC midnight, so accept the date observed on
	// either side of it instead of racing the clock.
	after := finance.Today()

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	wantDates := []string{before.Format(entryDateLayout), after.Format(entryDateLayout)}
	assert.Contains(t, wantDates, inputValue(t, recorder.Body.String(), "rate_date"), "rate date default")
}
