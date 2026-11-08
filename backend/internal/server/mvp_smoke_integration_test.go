//go:build integration

package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"budgettracker/internal/testdb"
)

// TestMVPWorkflowSmoke exercises the onboarding-to-report path through the
// public HTTP adapters: admin registration, household setup, invitation,
// member acceptance, entries, exchange rates, and a converted report.
func TestMVPWorkflowSmoke(t *testing.T) {
	pool := testdb.New(t)
	admin := newTestClientWithPool(t, pool)
	member := newTestClientWithPool(t, pool)

	adminCSRF := admin.registerHTML(t, "owner@example.com")
	homeID := createAPIHousehold(t, admin, adminCSRF, "Home")
	homePath := "/api/v1/households/" + homeID.String()

	response := admin.apiRequest(t, http.MethodPost, homePath+"/invitations", map[string]any{
		"email": "partner@example.com", "role": "member",
	}, adminCSRF)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("invite = %d, body:\n%s", response.StatusCode, readBody(t, response))
	}
	var invitation invitationEnvelope
	decodeBody(t, response, &invitation)

	memberCSRF := member.registerHTML(t, "partner@example.com")
	response = member.postForm(t, "/invitations/"+invitation.Token, url.Values{"csrf_token": {memberCSRF}})
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "You joined the household.") {
		t.Fatalf("accept invitation = %d, body:\n%s", response.StatusCode, body)
	}

	createRateAPI(t, admin, adminCSRF, map[string]any{
		"from_currency_code": "EUR", "to_currency_code": "RSD",
		"rate": "117.5", "rate_date": "2026-11-08",
	})
	createEntryAPI(t, admin, adminCSRF, "/api/v1/incomes", map[string]any{
		"amount": "100", "entry_date": "2026-11-08",
	})
	createEntryAPI(t, admin, adminCSRF, "/api/v1/spendings", map[string]any{
		"amount": "10", "currency_code": "EUR", "entry_date": "2026-11-08",
	})
	createEntryAPI(t, member, memberCSRF, "/api/v1/incomes", map[string]any{
		"amount": "20", "entry_date": "2026-11-08",
	})

	response = admin.apiRequest(t, http.MethodGet,
		"/api/v1/reports/summary?scope=all&from=2026-11-01&to=2026-11-30&currency=RSD", nil, "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("summary = %d, body:\n%s", response.StatusCode, readBody(t, response))
	}
	var summary summaryEnvelope
	decodeBody(t, response, &summary)
	if summary.Summary.IncomeMinor != 12000 ||
		summary.Summary.SpendingMinor != 117500 ||
		summary.Summary.NetMinor != -105500 ||
		!summary.Summary.Complete {
		t.Errorf("summary = %+v", summary.Summary)
	}

	response = member.apiRequest(t, http.MethodGet,
		"/api/v1/reports/summary?scope=mine&from=2026-11-01&to=2026-11-30&currency=RSD", nil, "")
	var memberSummary summaryEnvelope
	decodeBody(t, response, &memberSummary)
	if response.StatusCode != http.StatusOK || memberSummary.Summary.IncomeMinor != 2000 {
		t.Errorf("member summary = %d, %+v", response.StatusCode, memberSummary.Summary)
	}

	if _, err := pool.Exec(t.Context(), "SELECT 1"); err != nil {
		t.Fatalf("database health check: %v", err)
	}
}
