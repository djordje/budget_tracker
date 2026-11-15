//go:build integration

package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"

	"budgettracker/internal/testdb"
)

func TestRegisterLoginAndSessions(t *testing.T) {
	pool := testdb.New(t)
	service := NewService(pool, nil)
	ctx := context.Background()

	session, err := service.Register(ctx, RegisterParams{
		FirstName: "  Ada ",
		LastName:  " Lovelace ",
		Email:     " ADA@Example.COM ",
		Password:  "correct horse battery staple",
	})
	require.NoError(t, err, "Register() error")
	require.NotEmpty(t, session.Token, "Register() session token")
	assert.Equal(t, "ada@example.com", session.User.Email, "user email")
	assert.Equal(t, "Ada", session.User.FirstName, "user first name")
	assert.Equal(t, "Lovelace", session.User.LastName, "user last name")
	assert.Greater(t, time.Until(session.ExpiresAt), 29*24*time.Hour, "session expiry")

	var storedEmail string
	var storedHash string
	if err := pool.QueryRow(ctx, `
		SELECT c.email_normalized, c.password_hash
		FROM auth_credentials c
		WHERE c.user_id = $1`,
		session.User.ID,
	).Scan(&storedEmail, &storedHash); err != nil {
		require.NoError(t, err, "query stored credentials")
	}
	assert.Equal(t, "ada@example.com", storedEmail, "stored email")
	assert.NotEmpty(t, storedHash, "stored password hash")
	assert.NotEqual(t, "correct horse battery staple", storedHash, "password was not hashed")

	loaded, err := service.SessionByToken(ctx, session.Token)
	require.NoError(t, err, "SessionByToken() error")
	assert.Equal(t, session.User.ID, loaded.User.ID, "loaded session user")
	assert.Equal(t, "ada@example.com", loaded.User.Email, "loaded email")

	login, err := service.Login(ctx, "  ADA@example.com ", "correct horse battery staple")
	require.NoError(t, err, "Login() error")
	assert.NotEmpty(t, login.Token, "Login() token")
	assert.NotEqual(t, session.Token, login.Token, "Login() should issue a new token")

	_, err = service.Login(ctx, "ada@example.com", "wrong password")
	assert.ErrorIs(t, err, ErrInvalidCredentials, "Login() wrong password")
	_, err = service.Login(ctx, "missing@example.com", "correct horse battery staple")
	assert.ErrorIs(t, err, ErrInvalidCredentials, "Login() unknown email")
	_, err = service.Login(ctx, "not-an-email", "correct horse battery staple")
	assert.ErrorIs(t, err, ErrInvalidCredentials, "Login() invalid email")

	_, err = service.Register(ctx, RegisterParams{
		FirstName: "Another",
		LastName:  "Person",
		Email:     "Ada@Example.com",
		Password:  "another correct horse battery",
	})
	assert.ErrorIs(t, err, ErrEmailTaken, "Register() duplicate email")

	var userCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&userCount); err != nil {
		require.NoError(t, err, "count users")
	}
	assert.Equal(t, 1, userCount, "user count after failed duplicate registration")

	require.NoError(t, service.DeleteSession(ctx, session.Token), "DeleteSession()")

	_, err = service.SessionByToken(ctx, session.Token)
	assert.ErrorIs(t, err, ErrSessionNotFound, "SessionByToken() after delete")

	assert.NoError(t, service.DeleteSession(ctx, session.Token), "DeleteSession() twice")

	if _, err := pool.Exec(ctx,
		"UPDATE sessions SET expires_at = now() - interval '1 minute' WHERE token_hash = $1",
		hashSessionToken(login.Token),
	); err != nil {
		require.NoError(t, err, "expire session")
	}

	_, err = service.SessionByToken(ctx, login.Token)
	assert.ErrorIs(t, err, ErrSessionNotFound, "SessionByToken() expired")
	var expiredCount int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM sessions WHERE token_hash = $1",
		hashSessionToken(login.Token),
	).Scan(&expiredCount); err != nil {
		require.NoError(t, err, "count expired session")
	}
	assert.Zero(t, expiredCount, "expired session was not deleted")
}

func TestLoginRejectsUnknownEmailAndWrongPassword(t *testing.T) {
	pool := testdb.New(t)
	service := NewService(pool, nil)
	ctx := context.Background()

	registered, err := service.Register(ctx, RegisterParams{
		FirstName: "Ada",
		LastName:  "Lovelace",
		Email:     "ada@example.com",
		Password:  "correct horse battery staple",
	})
	require.NoError(t, err, "Register() error")

	_, wrongPasswordErr := service.Login(ctx, "ada@example.com", "wrong password")
	_, unknownEmailErr := service.Login(ctx, "missing@example.com", "correct horse battery staple")

	assert.ErrorIs(t, wrongPasswordErr, ErrInvalidCredentials, "Login() wrong password")
	assert.ErrorIs(t, unknownEmailErr, ErrInvalidCredentials, "Login() unknown email")

	var sessions int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id = $1", registered.User.ID).Scan(&sessions); err != nil {
		require.NoError(t, err, "count sessions")
	}
	assert.Equal(t, 1, sessions, "failed logins created a session")
}

func TestRegisterValidationDoesNotTouchDatabase(t *testing.T) {
	service := NewService(nil, nil)

	_, err := service.Register(context.Background(), RegisterParams{})
	require.Error(t, err, "Register() with empty params")
}

func TestSessionsUseTokenHashIdentityAndCleanupExpired(t *testing.T) {
	pool := testdb.New(t)
	service := NewService(pool, nil)
	ctx := context.Background()

	var idColumns int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'sessions' AND column_name = 'id'`).Scan(&idColumns); err != nil {
		require.NoError(t, err, "inspect sessions columns")
	}
	assert.Zero(t, idColumns, "sessions still has a separate id column")

	register := func(email string) Session {
		t.Helper()
		session, err := service.Register(ctx, RegisterParams{
			FirstName: "Test",
			LastName:  "User",
			Email:     email,
			Password:  "correct horse battery staple",
		})
		require.NoError(t, err, "Register(%s)", email)
		return session
	}

	expiredOne := register("expired-one@example.com")
	expiredTwo := register("expired-two@example.com")
	valid := register("valid@example.com")

	var stored int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM sessions WHERE token_hash = $1",
		hashSessionToken(valid.Token),
	).Scan(&stored); err != nil {
		require.NoError(t, err, "query session by token hash")
	}
	assert.Equal(t, 1, stored, "sessions keyed by token hash")

	if _, err := pool.Exec(ctx, `
		UPDATE sessions SET expires_at = now() - interval '1 minute'
		WHERE token_hash = ANY($1)`,
		[]string{hashSessionToken(expiredOne.Token), hashSessionToken(expiredTwo.Token)},
	); err != nil {
		require.NoError(t, err, "expire sessions")
	}

	removed, err := service.DeleteExpiredSessions(ctx)
	require.NoError(t, err, "DeleteExpiredSessions() error")
	assert.Equal(t, int64(2), removed, "removed expired sessions")

	var remaining int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions").Scan(&remaining); err != nil {
		require.NoError(t, err, "count sessions")
	}
	assert.Equal(t, 1, remaining, "remaining sessions")

	_, err = service.SessionByToken(ctx, valid.Token)
	assert.NoError(t, err, "SessionByToken(valid)")

	for _, token := range []string{expiredOne.Token, expiredTwo.Token} {
		_, err := service.SessionByToken(ctx, token)
		assert.ErrorIs(t, err, ErrSessionNotFound, "SessionByToken(expired)")
	}
}
