package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/households"
)

type failingDeleteEntryService struct {
	EntryService
	err    error
	entry  finance.Entry
	called bool
}

func (s *failingDeleteEntryService) Get(context.Context, uuid.UUID, finance.Kind, int64) (finance.Entry, error) {
	return s.entry, nil
}

func (s *failingDeleteEntryService) Delete(context.Context, uuid.UUID, finance.Kind, int64) error {
	s.called = true
	return s.err
}

type scopedDeleteEntryService struct {
	EntryService
	entry       finance.Entry
	deleteCalls int
}

func (s *scopedDeleteEntryService) Get(context.Context, uuid.UUID, finance.Kind, int64) (finance.Entry, error) {
	return s.entry, nil
}

func (s *scopedDeleteEntryService) Delete(context.Context, uuid.UUID, finance.Kind, int64) error {
	s.deleteCalls++
	return nil
}

func serveEntryDelete(
	t *testing.T,
	householdsService HouseholdService,
	entries EntryService,
	householdID uuid.UUID,
	htmx bool,
) *httptest.ResponseRecorder {
	t.Helper()
	handler, err := New(Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sessions:   stubSessions{},
		Households: householdsService,
		Entries:    entries,
		Rates:      stubRates{},
		Reports:    stubReports{},
	})
	require.NoError(t, err, "New()")

	userID := uuid.New()
	session := auth.Session{UserID: userID, User: auth.User{ID: userID}}
	request := httptest.NewRequest(http.MethodPost, householdURL(householdID, "/spendings/42/delete"), nil)
	request.SetPathValue("householdID", householdID.String())
	request.SetPathValue("entryID", "42")
	if htmx {
		request.Header.Set("HX-Request", "true")
	}
	request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
	recorder := httptest.NewRecorder()
	auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, slog.New(slog.NewTextHandler(io.Discard, nil))).
		Load(handler.Scoped("entries", "", handler.DeleteSpending)).ServeHTTP(recorder, request)
	return recorder
}

func TestHTMXDeleteFailuresReturnErrorFragments(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantText string
	}{
		{name: "forbidden", err: finance.ErrForbidden, wantText: "do not have permission"},
		{name: "missing", err: finance.ErrNotFound, wantText: "no longer available"},
		{name: "internal", err: errors.New("database unavailable"), wantText: "could not be deleted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			household := households.Household{ID: uuid.New(), Name: "Home", Role: households.RoleAdmin}
			entries := &failingDeleteEntryService{
				err:   tt.err,
				entry: finance.Entry{ID: 42, Kind: finance.KindSpending, HouseholdID: household.ID},
			}
			recorder := serveEntryDelete(t, stubHouseholds{households: []households.Household{household}}, entries, household.ID, true)

			assert.True(t, entries.called, "Delete() was not called")
			require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
			body := recorder.Body.String()
			assert.Contains(t, body, `id="entries-content"`, "body is not an entries-content fragment")
			assert.Contains(t, body, tt.wantText)
			assert.NotContains(t, body, "notice=entry_deleted", "failure fragment advertised a successful deletion")
			assert.NotContains(t, body, "Entry deleted", "failure fragment advertised a successful deletion")
			assert.NotContains(t, body, "<html", "failure returned a full page instead of a fragment")
		})
	}
}

func TestDeleteFailuresKeepRealStatusOnFullPages(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "forbidden", err: finance.ErrForbidden, wantStatus: http.StatusForbidden},
		{name: "missing", err: finance.ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "internal", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			household := households.Household{ID: uuid.New(), Name: "Home", Role: households.RoleAdmin}
			entries := &failingDeleteEntryService{
				err:   tt.err,
				entry: finance.Entry{ID: 42, Kind: finance.KindSpending, HouseholdID: household.ID},
			}
			recorder := serveEntryDelete(t, stubHouseholds{households: []households.Household{household}}, entries, household.ID, false)

			require.Equal(t, tt.wantStatus, recorder.Code, "body = %s", recorder.Body.String())
			assert.Contains(t, recorder.Body.String(), "<html", "failure did not return a full page")
		})
	}
}

func TestDeleteRejectsEntryFromAnotherHousehold(t *testing.T) {
	active := households.Household{ID: uuid.New(), Name: "Home", Role: households.RoleAdmin}
	other := households.Household{ID: uuid.New(), Name: "Other", Role: households.RoleAdmin}
	tests := []struct {
		name       string
		htmx       bool
		wantStatus int
		wantText   string
	}{
		{name: "page", wantStatus: http.StatusNotFound, wantText: "That entry is not available."},
		{name: "fragment", htmx: true, wantStatus: http.StatusOK, wantText: "That entry is no longer available."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := &scopedDeleteEntryService{entry: finance.Entry{
				ID:          42,
				Kind:        finance.KindSpending,
				HouseholdID: other.ID,
			}}
			recorder := serveEntryDelete(t, stubHouseholds{households: []households.Household{active}}, entries, active.ID, tt.htmx)

			require.Equal(t, tt.wantStatus, recorder.Code, "body = %s", recorder.Body.String())
			assert.Equal(t, 0, entries.deleteCalls, "Delete() calls")
			assert.Contains(t, recorder.Body.String(), tt.wantText)
		})
	}
}

func TestParseFormDateMatchesTheSharedParser(t *testing.T) {
	valid := time.Date(2026, time.October, 15, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		raw     string
		want    time.Time
		wantErr bool
	}{
		{name: "valid", raw: "2026-10-15", want: valid},
		{name: "empty", raw: ""},
		{name: "padded", raw: " 2026-10-15", wantErr: true},
		{name: "whitespace-only", raw: "   ", wantErr: true},
		{name: "malformed", raw: "15/10/2026", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFormDate(tt.raw)

			if tt.wantErr {
				assert.Error(t, err, "parseFormDate(%q)", tt.raw)
				return
			}
			require.NoError(t, err, "parseFormDate(%q)", tt.raw)
			assert.Equal(t, tt.want, got, "parseFormDate(%q)", tt.raw)
		})
	}
}

func TestParsePageMatchesTheJSONAdapter(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{name: "empty uses default", raw: "", want: 1},
		{name: "zero uses default", raw: "0", want: 0},
		{name: "first page", raw: "1", want: 1},
		{name: "negative", raw: "-1", wantErr: true},
		{name: "non-numeric", raw: "abc", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePage(tt.raw)

			if tt.wantErr {
				assert.Error(t, err, "parsePage(%q)", tt.raw)
				return
			}
			require.NoError(t, err, "parsePage(%q)", tt.raw)
			assert.Equal(t, tt.want, got, "parsePage(%q)", tt.raw)
		})
	}
}

func TestNewEntryFormDefaultsToUTCDate(t *testing.T) {
	before := finance.Today()
	recorder := serveHouseholdPage(t, func(h *Handler) http.HandlerFunc { return h.NewIncomePage })
	// A request can straddle UTC midnight, so accept the date observed on
	// either side of it instead of racing the clock.
	after := finance.Today()

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	wantDates := []string{before.Format(entryDateLayout), after.Format(entryDateLayout)}
	assert.Contains(t, wantDates, inputValue(t, recorder.Body.String(), "entry_date"), "entry date default")
}

// currencyEntryService provides an ordered catalog and recents to entry form
// handlers. The embedded interface keeps the remaining service contract.
type currencyEntryService struct {
	EntryService
	catalog []finance.Currency
	recent  []string
	entry   finance.Entry
}

func (s currencyEntryService) Currencies(context.Context) ([]finance.Currency, error) {
	return s.catalog, nil
}

func (s currencyEntryService) RecentCurrencies(context.Context, uuid.UUID, uuid.UUID) ([]string, error) {
	return s.recent, nil
}

func (s currencyEntryService) Get(context.Context, uuid.UUID, finance.Kind, int64) (finance.Entry, error) {
	return s.entry, nil
}

// currencyHouseholds serves one household whose effective spending default is
// configured by the test.
type currencyHouseholds struct {
	stubHouseholds
	view households.OptionsView
}

func (s currencyHouseholds) GetOptionsView(context.Context, uuid.UUID, uuid.UUID) (households.OptionsView, error) {
	return s.view, nil
}

// currencyOptions lists the currency codes of the rendered entry form select,
// in render order.
func currencyOptions(t *testing.T, body string) []string {
	t.Helper()
	start := strings.Index(body, `id="currency_code"`)
	require.NotEqual(t, -1, start, "currency select not rendered")
	rest := body[start:]
	end := strings.Index(rest, "</select>")
	require.NotEqual(t, -1, end, "currency select is not closed")
	matches := regexp.MustCompile(`<option value="([A-Z]{3})"`).FindAllStringSubmatch(rest[:end], -1)
	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}
	return codes
}

func serveCurrencyEntryForm(
	t *testing.T,
	entries EntryService,
	householdID uuid.UUID,
	method, path, form string,
	pathValues map[string]string,
	handle func(*Handler) http.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()
	handler, err := New(Deps{
		Logger:   discardLogger(),
		Sessions: stubSessions{},
		Households: currencyHouseholds{
			stubHouseholds: stubHouseholds{
				households: []households.Household{{ID: householdID, Name: "Home", Role: households.RoleAdmin}},
			},
			view: households.OptionsView{Effective: households.EffectiveOptions{
				DefaultIncomeCurrencyCode:   "RSD",
				DefaultSpendingCurrencyCode: "RSD",
				DefaultDisplayCurrencyCode:  "RSD",
			}},
		},
		Entries: entries,
		Rates:   stubRates{},
		Reports: stubReports{},
	})
	require.NoError(t, err, "New()")

	userID := uuid.New()
	session := auth.Session{
		UserID:            userID,
		User:              auth.User{ID: userID},
		ActiveHouseholdID: &householdID,
	}
	request := httptest.NewRequest(method, path, strings.NewReader(form))
	request.SetPathValue("householdID", householdID.String())
	for name, value := range pathValues {
		request.SetPathValue(name, value)
	}
	if form != "" {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
	recorder := httptest.NewRecorder()
	auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, discardLogger()).
		Load(handler.Scoped("entries", "", handle(handler))).
		ServeHTTP(recorder, request)
	return recorder
}

func TestNewEntryFormOrdersCurrenciesByRelevance(t *testing.T) {
	householdID := uuid.New()
	entries := currencyEntryService{
		catalog: []finance.Currency{{Code: "AUD"}, {Code: "EUR"}, {Code: "JPY"}, {Code: "RSD"}, {Code: "USD"}},
		recent:  []string{"EUR"},
	}

	recorder := serveCurrencyEntryForm(t, entries, householdID, http.MethodGet,
		householdURL(householdID, "/spendings/new"), "", nil,
		func(h *Handler) http.HandlerFunc { return h.NewSpendingPage })

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	body := recorder.Body.String()
	assert.Equal(t, []string{"RSD", "EUR", "AUD", "JPY", "USD"}, currencyOptions(t, body), "currency order")
	assert.Contains(t, body, `value="RSD" selected`, "default currency is not preselected")
}

func TestEntryFormPreservesSubmittedCurrencyOnValidationErrors(t *testing.T) {
	householdID := uuid.New()
	entries := currencyEntryService{
		catalog: []finance.Currency{{Code: "AUD"}, {Code: "EUR"}, {Code: "JPY"}, {Code: "RSD"}, {Code: "USD"}},
		recent:  []string{"EUR"},
	}

	recorder := serveCurrencyEntryForm(t, entries, householdID, http.MethodPost,
		householdURL(householdID, "/spendings"),
		"amount=1&currency_code=JPY&entry_date=15%2F10%2F2026", nil,
		func(h *Handler) http.HandlerFunc { return h.CreateSpending })

	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code, "body = %s", recorder.Body.String())
	body := recorder.Body.String()
	assert.Equal(t, []string{"JPY", "EUR", "AUD", "RSD", "USD"}, currencyOptions(t, body), "currency order")
	assert.Contains(t, body, `value="JPY" selected`, "submitted currency was not preserved")
}

func TestEditEntryFormPreservesEntryCurrency(t *testing.T) {
	householdID := uuid.New()
	entries := currencyEntryService{
		catalog: []finance.Currency{{Code: "AUD"}, {Code: "EUR"}, {Code: "JPY"}, {Code: "RSD"}, {Code: "USD"}},
		recent:  []string{"EUR"},
		// A household-wide entry is editable by the admin session below, so the
		// test isolates currency ordering and selection.
		entry: finance.Entry{
			ID:           42,
			Kind:         finance.KindSpending,
			HouseholdID:  householdID,
			AmountMinor:  150,
			CurrencyCode: "JPY",
		},
	}

	recorder := serveCurrencyEntryForm(t, entries, householdID, http.MethodGet,
		householdURL(householdID, "/spendings/42/edit"), "",
		map[string]string{"entryID": "42"},
		func(h *Handler) http.HandlerFunc { return h.EditSpendingPage })

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	body := recorder.Body.String()
	assert.Equal(t, []string{"JPY", "EUR", "AUD", "RSD", "USD"}, currencyOptions(t, body), "currency order")
	assert.Contains(t, body, `value="JPY" selected`, "entry currency is not selected")
}
