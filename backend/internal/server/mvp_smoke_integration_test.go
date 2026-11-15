//go:build integration

package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	if !assert.Equal(t, http.StatusCreated, response.StatusCode, "invite") {
		t.Fatalf("body:\n%s", readBody(t, response))
	}
	var invitation invitationEnvelope
	decodeBody(t, response, &invitation)

	memberCSRF := member.registerHTML(t, "partner@example.com")
	response = member.postForm(t, "/invitations/"+invitation.Token, url.Values{"csrf_token": {memberCSRF}})
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "accept invitation; body:\n%s", body)
	assert.Contains(t, body, "You joined the household.")

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
	if !assert.Equal(t, http.StatusOK, response.StatusCode, "summary") {
		t.Fatalf("body:\n%s", readBody(t, response))
	}
	var summary summaryEnvelope
	decodeBody(t, response, &summary)
	assert.Equal(t, int64(12000), summary.Summary.IncomeMinor, "summary = %+v", summary.Summary)
	assert.Equal(t, int64(117500), summary.Summary.SpendingMinor, "summary = %+v", summary.Summary)
	assert.Equal(t, int64(-105500), summary.Summary.NetMinor, "summary = %+v", summary.Summary)
	assert.True(t, summary.Summary.Complete, "summary = %+v", summary.Summary)

	response = member.apiRequest(t, http.MethodGet,
		"/api/v1/reports/summary?scope=mine&from=2026-11-01&to=2026-11-30&currency=RSD", nil, "")
	var memberSummary summaryEnvelope
	decodeBody(t, response, &memberSummary)
	assert.Equal(t, http.StatusOK, response.StatusCode, "member summary = %+v", memberSummary.Summary)
	assert.Equal(t, int64(2000), memberSummary.Summary.IncomeMinor, "member summary = %+v", memberSummary.Summary)

	if _, err := pool.Exec(t.Context(), "SELECT 1"); err != nil {
		require.NoError(t, err, "database health check")
	}
}
