//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"

	"budgettracker/internal/testdb"
)

type myOptionsEnvelope struct {
	Options struct {
		DefaultIncomeCurrencyCode   *string `json:"default_income_currency_code"`
		DefaultSpendingCurrencyCode *string `json:"default_spending_currency_code"`
		DefaultDisplayCurrencyCode  *string `json:"default_display_currency_code"`
	} `json:"options"`
	Effective struct {
		DefaultIncomeCurrencyCode   string `json:"default_income_currency_code"`
		DefaultSpendingCurrencyCode string `json:"default_spending_currency_code"`
		DefaultDisplayCurrencyCode  string `json:"default_display_currency_code"`
	} `json:"effective"`
}

type invitationEnvelope struct {
	Invitation struct {
		ID          string  `json:"id"`
		HouseholdID string  `json:"household_id"`
		Email       string  `json:"email"`
		Role        string  `json:"role"`
		ExpiresAt   string  `json:"expires_at"`
		AcceptedAt  *string `json:"accepted_at"`
	} `json:"invitation"`
	Token string `json:"token"`
}

type invitationsEnvelope struct {
	Invitations []struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"invitations"`
}

func TestOptionsAPI(t *testing.T) {
	pool := testdb.New(t)
	admin := newTestClientWithPool(t, pool)
	member := newTestClientWithPool(t, pool)
	outsider := newTestClientWithPool(t, pool)
	adminCSRF := admin.registerHTML(t, "admin@example.com")
	memberCSRFValue := member.registerHTML(t, "member@example.com")
	outsiderCSRFValue := outsider.registerHTML(t, "outsider@example.com")
	homeID := createAPIHousehold(t, admin, adminCSRF, "Home")
	memberID := userIDByEmail(t, pool, "member@example.com")
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, memberID,
	); err != nil {
		t.Fatalf("add member: %v", err)
	}

	response := admin.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/options", nil, "")
	var initial optionsEnvelope
	decodeBody(t, response, &initial)
	if response.StatusCode != http.StatusOK || initial.Options.DefaultIncomeCurrencyCode != "RSD" {
		t.Errorf("initial options = %d, %+v", response.StatusCode, initial.Options)
	}

	response = admin.apiRequest(t, http.MethodPut, "/api/v1/households/"+homeID.String()+"/options", map[string]any{
		"default_income_currency_code":   "EUR",
		"default_spending_currency_code": "USD",
		"default_display_currency_code":  "GBP",
	}, adminCSRF)
	var updated optionsEnvelope
	decodeBody(t, response, &updated)
	if response.StatusCode != http.StatusOK || updated.Options.DefaultDisplayCurrencyCode != "GBP" {
		t.Errorf("updated options = %d, %+v", response.StatusCode, updated.Options)
	}

	response = member.apiRequest(t, http.MethodPut, "/api/v1/households/"+homeID.String()+"/options", map[string]any{
		"default_income_currency_code":   "RSD",
		"default_spending_currency_code": "RSD",
		"default_display_currency_code":  "RSD",
	}, memberCSRFValue)
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("member update household options = %d, want 403", response.StatusCode)
	}
	response.Body.Close()

	response = outsider.apiRequest(t, http.MethodPut, "/api/v1/households/"+homeID.String()+"/options", map[string]any{
		"default_income_currency_code":   "RSD",
		"default_spending_currency_code": "RSD",
		"default_display_currency_code":  "RSD",
	}, outsiderCSRFValue)
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("outsider update household options = %d, want 404", response.StatusCode)
	}
	response.Body.Close()

	response = member.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/my-options", nil, "")
	var inherited myOptionsEnvelope
	decodeBody(t, response, &inherited)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("my options = %d", response.StatusCode)
	}
	if inherited.Options.DefaultIncomeCurrencyCode != nil || inherited.Effective.DefaultIncomeCurrencyCode != "EUR" {
		t.Errorf("inherited options = %+v", inherited)
	}

	response = member.apiRequest(t, http.MethodPut, "/api/v1/households/"+homeID.String()+"/my-options", map[string]any{
		"default_income_currency_code":   "RSD",
		"default_spending_currency_code": nil,
		"default_display_currency_code":  "EUR",
	}, memberCSRFValue)
	var overridden myOptionsEnvelope
	decodeBody(t, response, &overridden)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("update my options = %d", response.StatusCode)
	}
	if overridden.Options.DefaultIncomeCurrencyCode == nil || *overridden.Options.DefaultIncomeCurrencyCode != "RSD" {
		t.Errorf("income override = %v", overridden.Options.DefaultIncomeCurrencyCode)
	}
	if overridden.Options.DefaultSpendingCurrencyCode != nil {
		t.Errorf("spending override should inherit, got %v", overridden.Options.DefaultSpendingCurrencyCode)
	}
	if overridden.Effective.DefaultIncomeCurrencyCode != "RSD" ||
		overridden.Effective.DefaultSpendingCurrencyCode != "USD" ||
		overridden.Effective.DefaultDisplayCurrencyCode != "EUR" {
		t.Errorf("effective options = %+v", overridden.Effective)
	}
}

func TestInvitationAPI(t *testing.T) {
	pool := testdb.New(t)
	admin := newTestClientWithPool(t, pool)
	member := newTestClientWithPool(t, pool)
	outsider := newTestClientWithPool(t, pool)
	adminCSRF := admin.registerHTML(t, "admin@example.com")
	member.registerHTML(t, "member@example.com")
	outsiderCSRFValue := outsider.registerHTML(t, "outsider@example.com")
	homeID := createAPIHousehold(t, admin, adminCSRF, "Home")
	memberID := userIDByEmail(t, pool, "member@example.com")
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, memberID,
	); err != nil {
		t.Fatalf("add member: %v", err)
	}

	response := admin.apiRequest(t, http.MethodPost, "/api/v1/households/"+homeID.String()+"/invitations", map[string]any{
		"email": " New@Example.com ",
		"role":  "member",
	}, adminCSRF)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create invitation = %d, body:\n%s", response.StatusCode, readBody(t, response))
	}
	var created invitationEnvelope
	decodeBody(t, response, &created)
	if created.Token == "" || created.Invitation.Email != "new@example.com" || created.Invitation.Role != "member" {
		t.Errorf("created invitation = %+v, token=%q", created.Invitation, created.Token)
	}

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/invitations", nil, "")
	var listed invitationsEnvelope
	decodeBody(t, response, &listed)
	if response.StatusCode != http.StatusOK || len(listed.Invitations) != 1 {
		t.Errorf("listed invitations = %d, %+v", response.StatusCode, listed.Invitations)
	}

	response = member.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/invitations", nil, "")
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("member list invitations = %d, want 403", response.StatusCode)
	}
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/households/"+homeID.String()+"/invitations", map[string]any{
		"email": "new@example.com", "role": "member",
	}, adminCSRF)
	if response.StatusCode != http.StatusConflict {
		t.Errorf("duplicate invitation = %d, want 409", response.StatusCode)
	}
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/households/"+homeID.String()+"/invitations", map[string]any{
		"email": "member@example.com", "role": "member",
	}, adminCSRF)
	var memberConflict errorEnvelope
	decodeBody(t, response, &memberConflict)
	if response.StatusCode != http.StatusConflict || memberConflict.Error.Code != "already_member" {
		t.Errorf("existing member invite = %d, %+v", response.StatusCode, memberConflict.Error)
	}

	response = outsider.apiRequest(t, http.MethodPost, "/api/v1/households/"+homeID.String()+"/invitations", map[string]any{
		"email": "another@example.com", "role": "member",
	}, outsiderCSRFValue)
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("outsider invite = %d, want 404", response.StatusCode)
	}
	response.Body.Close()

	invitationID := created.Invitation.ID
	response = admin.apiRequest(t, http.MethodDelete, "/api/v1/households/"+homeID.String()+"/invitations/"+invitationID, nil, adminCSRF)
	if response.StatusCode != http.StatusNoContent {
		t.Errorf("revoke invitation = %d, want 204", response.StatusCode)
	}
	response.Body.Close()
}
