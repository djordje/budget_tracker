//go:build integration

package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgettracker/internal/testdb"
)

var invitationPathPattern = regexp.MustCompile(`/invitations/([A-Za-z0-9_-]+)`)

func invitationTokenFromBody(t *testing.T, body string) string {
	t.Helper()
	match := invitationPathPattern.FindStringSubmatch(body)
	require.Len(t, match, 2, "invitation link not found in body:\n%s", body)
	return match[1]
}

func TestInvitationHTMLFlow(t *testing.T) {
	pool := testdb.New(t)
	admin := newTestClientWithPool(t, pool)
	adminCSRF := admin.registerHTML(t, "admin@example.com")
	homeID := createAPIHousehold(t, admin, adminCSRF, "Home")
	householdBase := "/app/households/" + homeID.String()
	settingsPath := householdBase + "/settings"

	response, err := admin.follow.Get(admin.baseURL + settingsPath)
	require.NoError(t, err, "GET settings")
	body := readBody(t, response)
	require.Contains(t, body, "Invite someone", "settings invite form")
	require.Contains(t, body, "Currency defaults", "settings currency form")

	response = admin.postForm(t, householdBase+"/invitations", url.Values{
		"csrf_token": {adminCSRF},
		"email":      {"invitee@example.com"},
		"role":       {"member"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusCreated, response.StatusCode, "create invitation; body:\n%s", body)
	assert.Contains(t, body, "Invitation created.", "settings")
	assert.Contains(t, body, "invitee@example.com", "settings")
	token := invitationTokenFromBody(t, body)

	response = admin.postForm(t, householdBase+"/invitations", url.Values{
		"csrf_token": {adminCSRF},
		"email":      {"invitee@example.com"},
		"role":       {"member"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "duplicate invitation; body:\n%s", body)
	assert.Contains(t, body, "already exists")

	invitee := newTestClientWithPool(t, pool)
	nextPath := "/invitations/" + token
	registerPath := "/register?next=" + url.QueryEscape(nextPath)
	inviteeCSRF := invitee.csrfToken(t, registerPath)
	values := registerForm(inviteeCSRF)
	values.Set("email", "invitee@example.com")
	values.Set("next", nextPath)
	response = invitee.postForm(t, "/register", values)
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "registration next redirect; body:\n%s", body)
	assert.Contains(t, body, "Join Home")

	response = invitee.postForm(t, "/invitations/"+token, url.Values{"csrf_token": {inviteeCSRF}})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "accept invitation; body:\n%s", body)
	assert.Contains(t, body, "You joined the household.")
	assert.Contains(t, body, "Home")

	var memberRole string
	if err := pool.QueryRow(context.Background(), `
		SELECT role FROM household_memberships
		WHERE household_id = $1 AND user_id = (SELECT user_id FROM auth_credentials WHERE email_normalized = 'invitee@example.com')`,
		homeID,
	).Scan(&memberRole); err != nil {
		require.NoError(t, err, "look up accepted membership")
	}
	assert.Equal(t, "member", memberRole, "accepted role")

	response, err = invitee.follow.Get(invitee.baseURL + "/invitations/" + token)
	require.NoError(t, err, "GET accepted invitation")
	body = readBody(t, response)
	assert.Contains(t, body, "already been accepted", "accepted page")

	// A mismatched signed-in account cannot accept another invitation.
	response = admin.postForm(t, householdBase+"/invitations", url.Values{
		"csrf_token": {adminCSRF},
		"email":      {"other@example.com"},
		"role":       {"member"},
	})
	body = readBody(t, response)
	otherToken := invitationTokenFromBody(t, body)
	response, err = invitee.follow.Get(invitee.baseURL + "/invitations/" + otherToken)
	require.NoError(t, err, "GET mismatch invitation")
	body = readBody(t, response)
	assert.Contains(t, body, "This invitation is for other@example.com", "mismatch page")
	response = invitee.postForm(t, "/invitations/"+otherToken, url.Values{"csrf_token": {inviteeCSRF}})
	response.Body.Close()
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "mismatch accept")

	var invitationID string
	if err := pool.QueryRow(context.Background(), `
		SELECT id::text FROM household_invitations
		WHERE household_id = $1 AND email_normalized = 'other@example.com' AND accepted_at IS NULL`,
		homeID,
	).Scan(&invitationID); err != nil {
		require.NoError(t, err, "look up pending invitation")
	}
	response = admin.postForm(t, householdBase+"/invitations/"+invitationID+"/delete", url.Values{"csrf_token": {adminCSRF}})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "revoke invitation; body:\n%s", body)
	assert.Contains(t, body, "Invitation revoked.")
}

func TestOptionsAndPreferencesHTML(t *testing.T) {
	pool := testdb.New(t)
	admin := newTestClientWithPool(t, pool)
	adminCSRF := admin.registerHTML(t, "admin@example.com")
	homeID := createAPIHousehold(t, admin, adminCSRF, "Home")
	householdBase := "/app/households/" + homeID.String()

	response := admin.postForm(t, householdBase+"/options", url.Values{
		"csrf_token":                     {adminCSRF},
		"default_income_currency_code":   {"EUR"},
		"default_spending_currency_code": {"USD"},
		"default_display_currency_code":  {"GBP"},
	})
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "update options; body:\n%s", body)
	assert.Contains(t, body, "Household currency defaults saved.")

	member := newTestClientWithPool(t, pool)
	memberCSRF := member.registerHTML(t, "member@example.com")
	memberID := userIDByEmail(t, pool, "member@example.com")
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, memberID,
	); err != nil {
		require.NoError(t, err, "add member")
	}
	response = member.postForm(t, "/app/active-household", url.Values{
		"csrf_token":   {memberCSRF},
		"household_id": {homeID.String()},
	})
	readBody(t, response)

	response, err := member.follow.Get(member.baseURL + "/app/preferences")
	require.NoError(t, err, "GET preferences")
	body = readBody(t, response)
	assert.Contains(t, body, "Inherit (EUR)", "preferences")
	assert.Contains(t, body, "Inherit (GBP)", "preferences")

	response = member.postForm(t, "/app/preferences", url.Values{
		"csrf_token":                     {memberCSRF},
		"default_income_currency_code":   {"RSD"},
		"default_spending_currency_code": {""},
		"default_display_currency_code":  {"EUR"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "save preferences; body:\n%s", body)
	assert.Contains(t, body, "Preferences saved.")

	response, err = member.follow.Get(member.baseURL + "/app/spendings/new")
	require.NoError(t, err, "GET spending form")
	body = readBody(t, response)
	assert.Contains(t, body, `value="USD" selected`, "entry form did not inherit spending default")

	response, err = member.follow.Get(member.baseURL + "/app/reports")
	require.NoError(t, err, "GET reports")
	body = readBody(t, response)
	assert.Contains(t, body, `value="EUR" selected`, "report did not use display override")
}

func TestInvitationPageGuestFlow(t *testing.T) {
	pool := testdb.New(t)
	admin := newTestClientWithPool(t, pool)
	adminCSRF := admin.registerHTML(t, "admin@example.com")
	homeID := createAPIHousehold(t, admin, adminCSRF, "Home")
	householdBase := "/app/households/" + homeID.String()
	response := admin.postForm(t, householdBase+"/invitations", url.Values{
		"csrf_token": {adminCSRF}, "email": {"guest@example.com"}, "role": {"member"},
	})
	body := readBody(t, response)
	token := invitationTokenFromBody(t, body)

	guest := newTestClientWithPool(t, pool)
	response, err := guest.follow.Get(guest.baseURL + "/invitations/" + token)
	require.NoError(t, err, "GET guest invitation")
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "guest invitation; body:\n%s", body)
	assert.Contains(t, body, "Join Home")
	assert.Contains(t, body, "Create account")
	assert.Contains(t, body, `next=%2Finvitations%2F`+token, "guest invitation links missing next path")
}
