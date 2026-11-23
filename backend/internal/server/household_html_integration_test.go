//go:build integration

package server

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
)

func createHouseholdHTML(t *testing.T, client *testClient, csrf, name string) uuid.UUID {
	t.Helper()
	response := client.postForm(t, "/app/households", url.Values{
		"csrf_token": {csrf},
		"name":       {name},
	})
	body := readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "create household; body:\n%s", body)
	assert.Contains(t, body, "Household created.", "entries does not show creation notice")

	userID := userIDByEmail(t, client.pool, "ada@example.com")
	var householdID uuid.UUID
	if err := client.pool.QueryRow(context.Background(), `
		SELECT h.id
		FROM households h
		JOIN household_memberships m ON m.household_id = h.id
		WHERE m.user_id = $1 AND h.name = $2
		ORDER BY h.created_at DESC
		LIMIT 1`, userID, name,
	).Scan(&householdID); err != nil {
		require.NoError(t, err, "look up created household")
	}
	return householdID
}

func TestHouseholdHTMLRejectsInvalidUTF8Name(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")

	response := client.postForm(t, "/app/households", url.Values{
		"csrf_token": {csrf},
		"name":       {"B\xffd"},
	})
	body := readBody(t, response)

	require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "invalid UTF-8 household; body:\n%s", body)
	assert.Contains(t, body, "Household name contains invalid characters.", "field validation message")
}

func TestHouseholdHTMLFlow(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")

	response := client.postForm(t, "/app/households", url.Values{
		"csrf_token": {csrf},
		"name":       {""},
	})
	body := readBody(t, response)
	require.Equal(t, http.StatusUnprocessableEntity, response.StatusCode, "invalid household; body:\n%s", body)
	assert.Contains(t, body, "Household name is required.")

	homeID := createHouseholdHTML(t, client, csrf, "Home")
	settingsPath := "/app/" + homeID.String() + "/settings"

	response, err := client.follow.Get(client.baseURL + settingsPath)
	require.NoError(t, err, "GET settings")
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "GET settings; body:\n%s", body)
	for _, want := range []string{"Home", "Members", "ada@example.com", "RSD"} {
		assert.Contains(t, body, want, "settings")
	}

	response = client.postForm(t, settingsPath, url.Values{
		"csrf_token": {csrf},
		"name":       {"Shared home"},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "rename household; body:\n%s", body)
	assert.Contains(t, body, "Household settings saved.")
	assert.Contains(t, body, "Shared home")

	grace := newTestClientWithPool(t, client.pool)
	grace.registerHTML(t, "grace@example.com")

	response, err = grace.follow.Get(grace.baseURL + settingsPath)
	require.NoError(t, err, "grace GET settings")
	assert.NoError(t, response.Body.Close(), "close response body")
	assert.Equal(t, http.StatusNotFound, response.StatusCode, "non-member settings")

	graceID := userIDByEmail(t, client.pool, "grace@example.com")
	if _, err := client.pool.Exec(context.Background(), `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, graceID,
	); err != nil {
		require.NoError(t, err, "add member")
	}

	response, err = grace.follow.Get(grace.baseURL + settingsPath)
	require.NoError(t, err, "member GET settings")
	body = readBody(t, response)
	assert.Equal(t, http.StatusForbidden, response.StatusCode, "member settings; body:\n%s", body)
	assert.Contains(t, body, "Only household admins")

	response, err = client.follow.Get(client.baseURL + settingsPath)
	require.NoError(t, err, "admin GET settings")
	body = readBody(t, response)
	assert.Contains(t, body, "grace@example.com", "admin settings does not list member")

	createHouseholdHTML(t, client, csrf, "Second home")
	response, err = client.follow.Get(client.baseURL + "/app")
	require.NoError(t, err, "GET entries after second household")
	body = readBody(t, response)
	assert.Contains(t, body, "Second home", "second household was not made active")

	response = client.postForm(t, "/app/active-household", url.Values{
		"csrf_token":   {csrf},
		"household_id": {homeID.String()},
	})
	body = readBody(t, response)
	require.Equal(t, http.StatusOK, response.StatusCode, "switch household; body:\n%s", body)
	assert.Equal(t, "/app/"+homeID.String()+"/entries", response.Request.URL.Path)
	assert.Contains(t, body, "Shared home")
}

func TestHouseholdHTMLRequiresAuth(t *testing.T) {
	client := newTestClient(t)
	response, err := client.noRedirect.Get(client.baseURL + "/app/households/new")
	require.NoError(t, err, "GET new household")
	assert.NoError(t, response.Body.Close(), "close response body")
	assert.Equal(t, http.StatusSeeOther, response.StatusCode, "GET new household")
	assert.Equal(t, "/login?next="+url.QueryEscape("/app/households/new"), response.Header.Get("Location"), "Location")
}
