//go:build integration

package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"budgettracker/internal/testdb"
)

var invitationPathPattern = regexp.MustCompile(`/invitations/([A-Za-z0-9_-]+)`)

func invitationTokenFromBody(t *testing.T, body string) string {
	t.Helper()
	match := invitationPathPattern.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("invitation link not found in body:\n%s", body)
	}
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
	if err != nil {
		t.Fatalf("GET settings: %v", err)
	}
	body := readBody(t, response)
	if !strings.Contains(body, "Invite someone") || !strings.Contains(body, "Currency defaults") {
		t.Fatalf("settings missing invite/currency forms:\n%s", body)
	}

	response = admin.postForm(t, householdBase+"/invitations", url.Values{
		"csrf_token": {adminCSRF},
		"email":      {"invitee@example.com"},
		"role":       {"member"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create invitation = %d, body:\n%s", response.StatusCode, body)
	}
	if !strings.Contains(body, "Invitation created.") || !strings.Contains(body, "invitee@example.com") {
		t.Errorf("settings missing created invitation:\n%s", body)
	}
	token := invitationTokenFromBody(t, body)

	response = admin.postForm(t, householdBase+"/invitations", url.Values{
		"csrf_token": {adminCSRF},
		"email":      {"invitee@example.com"},
		"role":       {"member"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "already exists") {
		t.Fatalf("duplicate invitation = %d, body:\n%s", response.StatusCode, body)
	}

	invitee := newTestClientWithPool(t, pool)
	nextPath := "/invitations/" + token
	registerPath := "/register?next=" + url.QueryEscape(nextPath)
	inviteeCSRF := invitee.csrfToken(t, registerPath)
	values := registerForm(inviteeCSRF)
	values.Set("email", "invitee@example.com")
	values.Set("next", nextPath)
	response = invitee.postForm(t, "/register", values)
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Join Home") {
		t.Fatalf("registration next redirect = %d, body:\n%s", response.StatusCode, body)
	}

	response = invitee.postForm(t, "/invitations/"+token, url.Values{"csrf_token": {inviteeCSRF}})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "You joined the household.") || !strings.Contains(body, "Home") {
		t.Fatalf("accept invitation = %d, body:\n%s", response.StatusCode, body)
	}

	var memberRole string
	if err := pool.QueryRow(context.Background(), `
		SELECT role FROM household_memberships
		WHERE household_id = $1 AND user_id = (SELECT user_id FROM auth_credentials WHERE email_normalized = 'invitee@example.com')`,
		homeID,
	).Scan(&memberRole); err != nil {
		t.Fatalf("look up accepted membership: %v", err)
	}
	if memberRole != "member" {
		t.Errorf("accepted role = %q, want member", memberRole)
	}

	response, err = invitee.follow.Get(invitee.baseURL + "/invitations/" + token)
	if err != nil {
		t.Fatalf("GET accepted invitation: %v", err)
	}
	body = readBody(t, response)
	if !strings.Contains(body, "already been accepted") {
		t.Errorf("accepted page missing status:\n%s", body)
	}

	// A mismatched signed-in account cannot accept another invitation.
	response = admin.postForm(t, householdBase+"/invitations", url.Values{
		"csrf_token": {adminCSRF},
		"email":      {"other@example.com"},
		"role":       {"member"},
	})
	body = readBody(t, response)
	otherToken := invitationTokenFromBody(t, body)
	response, err = invitee.follow.Get(invitee.baseURL + "/invitations/" + otherToken)
	if err != nil {
		t.Fatalf("GET mismatch invitation: %v", err)
	}
	body = readBody(t, response)
	if !strings.Contains(body, "This invitation is for other@example.com") {
		t.Errorf("mismatch page missing email:\n%s", body)
	}
	response = invitee.postForm(t, "/invitations/"+otherToken, url.Values{"csrf_token": {inviteeCSRF}})
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("mismatch accept = %d, want 403", response.StatusCode)
	}

	var invitationID string
	if err := pool.QueryRow(context.Background(), `
		SELECT id::text FROM household_invitations
		WHERE household_id = $1 AND email_normalized = 'other@example.com' AND accepted_at IS NULL`,
		homeID,
	).Scan(&invitationID); err != nil {
		t.Fatalf("look up pending invitation: %v", err)
	}
	response = admin.postForm(t, householdBase+"/invitations/"+invitationID+"/delete", url.Values{"csrf_token": {adminCSRF}})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Invitation revoked.") {
		t.Fatalf("revoke invitation = %d, body:\n%s", response.StatusCode, body)
	}
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
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Household currency defaults saved.") {
		t.Fatalf("update options = %d, body:\n%s", response.StatusCode, body)
	}

	member := newTestClientWithPool(t, pool)
	memberCSRF := member.registerHTML(t, "member@example.com")
	memberID := userIDByEmail(t, pool, "member@example.com")
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, memberID,
	); err != nil {
		t.Fatalf("add member: %v", err)
	}
	response = member.postForm(t, "/app/active-household", url.Values{
		"csrf_token":   {memberCSRF},
		"household_id": {homeID.String()},
	})
	readBody(t, response)

	response, err := member.follow.Get(member.baseURL + "/app/preferences")
	if err != nil {
		t.Fatalf("GET preferences: %v", err)
	}
	body = readBody(t, response)
	if !strings.Contains(body, "Inherit (EUR)") || !strings.Contains(body, "Inherit (GBP)") {
		t.Errorf("preferences missing inherited defaults:\n%s", body)
	}

	response = member.postForm(t, "/app/preferences", url.Values{
		"csrf_token":                     {memberCSRF},
		"default_income_currency_code":   {"RSD"},
		"default_spending_currency_code": {""},
		"default_display_currency_code":  {"EUR"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Preferences saved.") {
		t.Fatalf("save preferences = %d, body:\n%s", response.StatusCode, body)
	}

	response, err = member.follow.Get(member.baseURL + "/app/spendings/new")
	if err != nil {
		t.Fatalf("GET spending form: %v", err)
	}
	body = readBody(t, response)
	if !strings.Contains(body, `value="USD" selected`) {
		t.Errorf("entry form did not inherit spending default:\n%s", body)
	}

	response, err = member.follow.Get(member.baseURL + "/app/reports")
	if err != nil {
		t.Fatalf("GET reports: %v", err)
	}
	body = readBody(t, response)
	if !strings.Contains(body, `value="EUR" selected`) {
		t.Errorf("report did not use display override:\n%s", body)
	}
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
	if err != nil {
		t.Fatalf("GET guest invitation: %v", err)
	}
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Join Home") || !strings.Contains(body, "Create account") {
		t.Fatalf("guest invitation = %d, body:\n%s", response.StatusCode, body)
	}
	if !strings.Contains(body, `next=%2Finvitations%2F`+token) {
		t.Errorf("guest invitation links missing next path:\n%s", body)
	}
}
