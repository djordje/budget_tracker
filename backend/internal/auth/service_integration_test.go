//go:build integration

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"budgettracker/internal/testdb"
)

func TestRegisterLoginAndSessions(t *testing.T) {
	pool := testdb.New(t)
	service := NewService(pool)
	ctx := context.Background()

	session, err := service.Register(ctx, RegisterParams{
		FirstName: "  Ada ",
		LastName:  " Lovelace ",
		Email:     " ADA@Example.COM ",
		Password:  "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if session.Token == "" {
		t.Fatal("Register() returned an empty session token")
	}
	if session.User.Email != "ada@example.com" {
		t.Errorf("user email = %q, want ada@example.com", session.User.Email)
	}
	if session.User.FirstName != "Ada" || session.User.LastName != "Lovelace" {
		t.Errorf("user name = %q %q", session.User.FirstName, session.User.LastName)
	}
	if time.Until(session.ExpiresAt) <= 29*24*time.Hour {
		t.Errorf("session expiry = %v, want about 30 days", session.ExpiresAt)
	}

	var storedEmail string
	var storedHash string
	if err := pool.QueryRow(ctx, `
		SELECT c.email_normalized, c.password_hash
		FROM auth_credentials c
		WHERE c.user_id = $1`,
		session.User.ID,
	).Scan(&storedEmail, &storedHash); err != nil {
		t.Fatalf("query stored credentials: %v", err)
	}
	if storedEmail != "ada@example.com" {
		t.Errorf("stored email = %q", storedEmail)
	}
	if storedHash == "correct horse battery staple" || storedHash == "" {
		t.Error("password was not hashed")
	}

	loaded, err := service.SessionByToken(ctx, session.Token)
	if err != nil {
		t.Fatalf("SessionByToken() error = %v", err)
	}
	if loaded.User.ID != session.User.ID {
		t.Errorf("loaded session user = %s, want %s", loaded.User.ID, session.User.ID)
	}
	if loaded.User.Email != "ada@example.com" {
		t.Errorf("loaded email = %q", loaded.User.Email)
	}

	login, err := service.Login(ctx, "  ADA@example.com ", "correct horse battery staple")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if login.Token == "" || login.Token == session.Token {
		t.Errorf("Login() token = %q, want a new token", login.Token)
	}

	if _, err := service.Login(ctx, "ada@example.com", "wrong password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login() wrong password error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := service.Login(ctx, "missing@example.com", "correct horse battery staple"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login() unknown email error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := service.Login(ctx, "not-an-email", "correct horse battery staple"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login() invalid email error = %v, want ErrInvalidCredentials", err)
	}

	if _, err := service.Register(ctx, RegisterParams{
		FirstName: "Another",
		LastName:  "Person",
		Email:     "Ada@Example.com",
		Password:  "another correct horse battery",
	}); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("Register() duplicate error = %v, want ErrEmailTaken", err)
	}

	var userCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Errorf("user count = %d, want 1 after failed duplicate registration", userCount)
	}

	if err := service.DeleteSession(ctx, session.Token); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := service.SessionByToken(ctx, session.Token); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("SessionByToken() after delete error = %v, want ErrSessionNotFound", err)
	}
	if err := service.DeleteSession(ctx, session.Token); err != nil {
		t.Errorf("DeleteSession() twice error = %v", err)
	}

	if _, err := pool.Exec(ctx,
		"UPDATE sessions SET expires_at = now() - interval '1 minute' WHERE token_hash = $1",
		hashSessionToken(login.Token),
	); err != nil {
		t.Fatalf("expire session: %v", err)
	}
	if _, err := service.SessionByToken(ctx, login.Token); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("SessionByToken() expired error = %v, want ErrSessionNotFound", err)
	}
	var expiredCount int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM sessions WHERE token_hash = $1",
		hashSessionToken(login.Token),
	).Scan(&expiredCount); err != nil {
		t.Fatalf("count expired session: %v", err)
	}
	if expiredCount != 0 {
		t.Error("expired session was not deleted")
	}
}

func TestRegisterValidationDoesNotTouchDatabase(t *testing.T) {
	service := NewService(nil)

	if _, err := service.Register(context.Background(), RegisterParams{}); err == nil {
		t.Fatal("Register() error = nil, want validation error")
	}
}

func TestSessionsUseTokenHashIdentityAndCleanupExpired(t *testing.T) {
	pool := testdb.New(t)
	service := NewService(pool)
	ctx := context.Background()

	var idColumns int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'sessions' AND column_name = 'id'`).Scan(&idColumns); err != nil {
		t.Fatalf("inspect sessions columns: %v", err)
	}
	if idColumns != 0 {
		t.Error("sessions still has a separate id column")
	}

	register := func(email string) Session {
		t.Helper()
		session, err := service.Register(ctx, RegisterParams{
			FirstName: "Test",
			LastName:  "User",
			Email:     email,
			Password:  "correct horse battery staple",
		})
		if err != nil {
			t.Fatalf("Register(%s) error = %v", email, err)
		}
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
		t.Fatalf("query session by token hash: %v", err)
	}
	if stored != 1 {
		t.Errorf("sessions keyed by token hash = %d, want 1", stored)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE sessions SET expires_at = now() - interval '1 minute'
		WHERE token_hash = ANY($1)`,
		[]string{hashSessionToken(expiredOne.Token), hashSessionToken(expiredTwo.Token)},
	); err != nil {
		t.Fatalf("expire sessions: %v", err)
	}

	removed, err := service.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions() error = %v", err)
	}
	if removed != 2 {
		t.Errorf("removed %d expired sessions, want 2", removed)
	}

	var remaining int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions").Scan(&remaining); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if remaining != 1 {
		t.Errorf("remaining sessions = %d, want 1", remaining)
	}

	if _, err := service.SessionByToken(ctx, valid.Token); err != nil {
		t.Errorf("SessionByToken(valid) error = %v", err)
	}
	for _, token := range []string{expiredOne.Token, expiredTwo.Token} {
		if _, err := service.SessionByToken(ctx, token); !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("SessionByToken(expired) error = %v, want ErrSessionNotFound", err)
		}
	}
}
