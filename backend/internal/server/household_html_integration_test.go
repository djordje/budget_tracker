//go:build integration

package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func createHouseholdHTML(t *testing.T, client *testClient, csrf, name string) uuid.UUID {
	t.Helper()
	response := client.postForm(t, "/app/households", url.Values{
		"csrf_token": {csrf},
		"name":       {name},
	})
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("create household = %d, body:\n%s", response.StatusCode, body)
	}
	if !strings.Contains(body, "Household created.") {
		t.Errorf("dashboard does not show creation notice:\n%s", body)
	}

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
		t.Fatalf("look up created household: %v", err)
	}
	return householdID
}

func TestHouseholdHTMLFlow(t *testing.T) {
	client := newTestClient(t)
	csrf := client.registerHTML(t, "ada@example.com")

	response := client.postForm(t, "/app/households", url.Values{
		"csrf_token": {csrf},
		"name":       {""},
	})
	body := readBody(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Household name is required.") {
		t.Fatalf("invalid household = %d, body:\n%s", response.StatusCode, body)
	}

	homeID := createHouseholdHTML(t, client, csrf, "Home")
	settingsPath := "/app/households/" + homeID.String() + "/settings"

	response, err := client.follow.Get(client.baseURL + settingsPath)
	if err != nil {
		t.Fatalf("GET settings: %v", err)
	}
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET settings = %d, body:\n%s", response.StatusCode, body)
	}
	for _, want := range []string{"Home", "Members", "ada@example.com", "RSD"} {
		if !strings.Contains(body, want) {
			t.Errorf("settings does not contain %q", want)
		}
	}

	response = client.postForm(t, settingsPath, url.Values{
		"csrf_token": {csrf},
		"name":       {"Shared home"},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, "Household settings saved.") || !strings.Contains(body, "Shared home") {
		t.Fatalf("rename household = %d, body:\n%s", response.StatusCode, body)
	}

	grace := newTestClientWithPool(t, client.pool)
	grace.registerHTML(t, "grace@example.com")

	response, err = grace.follow.Get(grace.baseURL + settingsPath)
	if err != nil {
		t.Fatalf("grace GET settings: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("non-member settings = %d, want 404", response.StatusCode)
	}

	graceID := userIDByEmail(t, client.pool, "grace@example.com")
	if _, err := client.pool.Exec(context.Background(), `
		INSERT INTO household_memberships (household_id, user_id, role)
		VALUES ($1, $2, 'member')`, homeID, graceID,
	); err != nil {
		t.Fatalf("add member: %v", err)
	}

	response, err = grace.follow.Get(grace.baseURL + settingsPath)
	if err != nil {
		t.Fatalf("member GET settings: %v", err)
	}
	body = readBody(t, response)
	if response.StatusCode != http.StatusForbidden || !strings.Contains(body, "Only household admins") {
		t.Errorf("member settings = %d, body:\n%s", response.StatusCode, body)
	}

	response, err = client.follow.Get(client.baseURL + settingsPath)
	if err != nil {
		t.Fatalf("admin GET settings: %v", err)
	}
	body = readBody(t, response)
	if !strings.Contains(body, "grace@example.com") {
		t.Errorf("admin settings does not list member:\n%s", body)
	}

	createHouseholdHTML(t, client, csrf, "Second home")
	response, err = client.follow.Get(client.baseURL + "/app")
	if err != nil {
		t.Fatalf("GET dashboard after second household: %v", err)
	}
	body = readBody(t, response)
	if !strings.Contains(body, "Second home") {
		t.Errorf("second household was not made active:\n%s", body)
	}

	response = client.postForm(t, "/app/active-household", url.Values{
		"csrf_token":   {csrf},
		"household_id": {homeID.String()},
	})
	body = readBody(t, response)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, ">Shared home</h1>") {
		t.Fatalf("switch household = %d, body:\n%s", response.StatusCode, body)
	}
	if strings.Contains(body, ">Second home</h1>") {
		t.Errorf("dashboard still shows the previous household:\n%s", body)
	}
}

func TestHouseholdHTMLRequiresAuth(t *testing.T) {
	client := newTestClient(t)
	response, err := client.noRedirect.Get(client.baseURL + "/app/households/new")
	if err != nil {
		t.Fatalf("GET new household: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/login" {
		t.Errorf("GET new household = %d %q, want 303 /login", response.StatusCode, response.Header.Get("Location"))
	}
}
