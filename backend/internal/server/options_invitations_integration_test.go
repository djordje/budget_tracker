//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		require.NoError(t, err, "add member")
	}

	response := admin.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/options", nil, "")
	var initial optionsEnvelope
	decodeBody(t, response, &initial)
	assert.Equal(t, http.StatusOK, response.StatusCode, "initial options = %+v", initial.Options)
	assert.Equal(t, "RSD", initial.Options.DefaultIncomeCurrencyCode, "initial options = %+v", initial.Options)

	response = admin.apiRequest(t, http.MethodPut, "/api/v1/households/"+homeID.String()+"/options", map[string]any{
		"default_income_currency_code":   "EUR",
		"default_spending_currency_code": "USD",
		"default_display_currency_code":  "GBP",
	}, adminCSRF)
	var updated optionsEnvelope
	decodeBody(t, response, &updated)
	assert.Equal(t, http.StatusOK, response.StatusCode, "updated options = %+v", updated.Options)
	assert.Equal(t, "GBP", updated.Options.DefaultDisplayCurrencyCode, "updated options = %+v", updated.Options)

	response = member.apiRequest(t, http.MethodPut, "/api/v1/households/"+homeID.String()+"/options", map[string]any{
		"default_income_currency_code":   "RSD",
		"default_spending_currency_code": "RSD",
		"default_display_currency_code":  "RSD",
	}, memberCSRFValue)
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "member update household options")
	response.Body.Close()

	response = outsider.apiRequest(t, http.MethodPut, "/api/v1/households/"+homeID.String()+"/options", map[string]any{
		"default_income_currency_code":   "RSD",
		"default_spending_currency_code": "RSD",
		"default_display_currency_code":  "RSD",
	}, outsiderCSRFValue)
	assert.Equal(t, http.StatusNotFound, response.StatusCode, "outsider update household options")
	response.Body.Close()

	response = member.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/my-options", nil, "")
	var inherited myOptionsEnvelope
	decodeBody(t, response, &inherited)
	require.Equal(t, http.StatusOK, response.StatusCode, "my options")
	assert.Nil(t, inherited.Options.DefaultIncomeCurrencyCode, "inherited options = %+v", inherited)
	assert.Equal(t, "EUR", inherited.Effective.DefaultIncomeCurrencyCode, "inherited options = %+v", inherited)

	response = member.apiRequest(t, http.MethodPut, "/api/v1/households/"+homeID.String()+"/my-options", map[string]any{
		"default_income_currency_code":   "RSD",
		"default_spending_currency_code": nil,
		"default_display_currency_code":  "EUR",
	}, memberCSRFValue)
	var overridden myOptionsEnvelope
	decodeBody(t, response, &overridden)
	require.Equal(t, http.StatusOK, response.StatusCode, "update my options")
	require.NotNil(t, overridden.Options.DefaultIncomeCurrencyCode, "income override")
	assert.Equal(t, "RSD", *overridden.Options.DefaultIncomeCurrencyCode, "income override")
	assert.Nil(t, overridden.Options.DefaultSpendingCurrencyCode, "spending override should inherit")
	assert.Equal(t, "RSD", overridden.Effective.DefaultIncomeCurrencyCode, "effective options")
	assert.Equal(t, "USD", overridden.Effective.DefaultSpendingCurrencyCode, "effective options")
	assert.Equal(t, "EUR", overridden.Effective.DefaultDisplayCurrencyCode, "effective options")
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
		require.NoError(t, err, "add member")
	}

	response := admin.apiRequest(t, http.MethodPost, "/api/v1/households/"+homeID.String()+"/invitations", map[string]any{
		"email": " New@Example.com ",
		"role":  "member",
	}, adminCSRF)
	if !assert.Equal(t, http.StatusCreated, response.StatusCode, "create invitation") {
		t.Fatalf("body:\n%s", readBody(t, response))
	}
	var created invitationEnvelope
	decodeBody(t, response, &created)
	assert.NotEmpty(t, created.Token, "created invitation = %+v", created.Invitation)
	assert.Equal(t, "new@example.com", created.Invitation.Email, "created invitation = %+v", created.Invitation)
	assert.Equal(t, "member", created.Invitation.Role, "created invitation = %+v", created.Invitation)

	response = admin.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/invitations", nil, "")
	var listed invitationsEnvelope
	decodeBody(t, response, &listed)
	assert.Equal(t, http.StatusOK, response.StatusCode, "listed invitations = %+v", listed.Invitations)
	assert.Len(t, listed.Invitations, 1, "listed invitations = %+v", listed.Invitations)

	response = member.apiRequest(t, http.MethodGet, "/api/v1/households/"+homeID.String()+"/invitations", nil, "")
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "member list invitations")
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/households/"+homeID.String()+"/invitations", map[string]any{
		"email": "new@example.com", "role": "member",
	}, adminCSRF)
	assert.Equal(t, http.StatusConflict, response.StatusCode, "duplicate invitation")
	response.Body.Close()

	response = admin.apiRequest(t, http.MethodPost, "/api/v1/households/"+homeID.String()+"/invitations", map[string]any{
		"email": "member@example.com", "role": "member",
	}, adminCSRF)
	var memberConflict errorEnvelope
	decodeBody(t, response, &memberConflict)
	assert.Equal(t, http.StatusConflict, response.StatusCode, "existing member invite = %+v", memberConflict.Error)
	assert.Equal(t, "already_member", memberConflict.Error.Code, "existing member invite")

	response = outsider.apiRequest(t, http.MethodPost, "/api/v1/households/"+homeID.String()+"/invitations", map[string]any{
		"email": "another@example.com", "role": "member",
	}, outsiderCSRFValue)
	assert.Equal(t, http.StatusNotFound, response.StatusCode, "outsider invite")
	response.Body.Close()

	invitationID := created.Invitation.ID
	response = admin.apiRequest(t, http.MethodDelete, "/api/v1/households/"+homeID.String()+"/invitations/"+invitationID, nil, adminCSRF)
	assert.Equal(t, http.StatusNoContent, response.StatusCode, "revoke invitation")
	response.Body.Close()
}
