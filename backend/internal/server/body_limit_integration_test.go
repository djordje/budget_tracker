//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgettracker/internal/testdb"
)

// oversizedBodyBytes exceeds both the HTML form and JSON body limits.
const oversizedBodyBytes = 2 << 20

func TestOversizedHTMLFormBodyIsRejected(t *testing.T) {
	pool := testdb.New(t)
	client := newTestClientWithPool(t, pool)

	values := registerForm(client.csrfToken(t, "/register"))
	values.Set("email", "flood@example.com")
	values.Set("first_name", strings.Repeat("a", oversizedBodyBytes))
	response := client.postForm(t, "/register", values)
	readBody(t, response)

	require.Equal(t, http.StatusRequestEntityTooLarge, response.StatusCode, "oversized form status")

	var count int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT count(*) FROM auth_credentials WHERE email_normalized = $1`, "flood@example.com",
	).Scan(&count), "count registered users")
	assert.Zero(t, count, "oversized form body reached the register handler")
}

func TestOversizedJSONBodyKeepsAdapterError(t *testing.T) {
	pool := testdb.New(t)
	client := newTestClientWithPool(t, pool)
	csrf := client.registerHTML(t, "owner@example.com")

	response := client.apiRequest(t, http.MethodPost, "/api/v1/households",
		map[string]any{"name": strings.Repeat("a", oversizedBodyBytes)}, csrf)
	var envelope errorEnvelope
	decodeBody(t, response, &envelope)

	require.Equal(t, http.StatusRequestEntityTooLarge, response.StatusCode, "error = %+v", envelope.Error)
	assert.Equal(t, "request_too_large", envelope.Error.Code, "error code")
}

func TestChunkedOversizedHTMLFormBodyIsRejected(t *testing.T) {
	pool := testdb.New(t)
	client := newTestClientWithPool(t, pool)

	values := registerForm(client.csrfToken(t, "/register"))
	values.Set("email", "chunked@example.com")
	values.Set("first_name", strings.Repeat("a", oversizedBodyBytes))

	request, err := http.NewRequest(http.MethodPost, client.baseURL+"/register", strings.NewReader(values.Encode()))
	require.NoError(t, err, "create chunked POST request")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.ContentLength = -1
	response, err := client.noRedirect.Do(request)
	require.NoError(t, err, "POST /register")
	body := readBody(t, response)

	require.Equal(t, http.StatusRequestEntityTooLarge, response.StatusCode, "body = %s", body)

	var count int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT count(*) FROM auth_credentials WHERE email_normalized = $1`, "chunked@example.com",
	).Scan(&count), "count registered users")
	assert.Zero(t, count, "chunked oversized form body reached the register handler")
}
