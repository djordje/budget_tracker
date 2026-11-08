package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgettracker/internal/store"
)

// SessionLifetime is how long a new session remains valid.
const SessionLifetime = 30 * 24 * time.Hour

// sessionCleanupInterval bounds how often a process opportunistically removes
// expired sessions.
const sessionCleanupInterval = time.Hour

var (
	// ErrInvalidCredentials deliberately does not distinguish between an
	// unknown email and an incorrect password.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrEmailTaken indicates that a normalized email already has credentials.
	ErrEmailTaken = errors.New("email already registered")
	// ErrSessionNotFound indicates that a token is missing, revoked, or expired.
	ErrSessionNotFound = errors.New("session not found")
	// ErrHouseholdAccess indicates that a session cannot select a household the
	// user does not belong to.
	ErrHouseholdAccess = errors.New("household access denied")
)

// User is the public profile attached to a session.
type User struct {
	ID        uuid.UUID
	FirstName string
	LastName  string
	Email     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Session represents an authenticated browser or API session. Token is only
// populated when a session is created; lookups return an empty Token.
type Session struct {
	UserID            uuid.UUID
	Token             string
	TokenHash         string
	User              User
	ExpiresAt         time.Time
	ActiveHouseholdID *uuid.UUID
}

// RegisterParams contains the fields accepted by registration.
type RegisterParams struct {
	FirstName string
	LastName  string
	Email     string
	Password  string
}

// Service owns credential, registration, login, and session persistence.
type Service struct {
	pool *pgxpool.Pool

	cleanupMu   sync.Mutex
	lastCleanup time.Time
	now         func() time.Time
	cleanup     func(context.Context) (int64, error)
}

// NewService constructs an authentication service.
func NewService(pool *pgxpool.Pool) *Service {
	service := &Service{pool: pool, now: time.Now}
	service.cleanup = service.DeleteExpiredSessions
	return service
}

// Register creates a user, credentials, and a session in one transaction.
func (s *Service) Register(ctx context.Context, params RegisterParams) (Session, error) {
	email, validation := validateRegistration(params)
	if validation != nil {
		return Session{}, validation
	}

	passwordHash, err := HashPassword(params.Password)
	if err != nil {
		return Session{}, fmt.Errorf("hash password: %w", err)
	}
	rawToken, tokenHash, err := newSessionToken()
	if err != nil {
		return Session{}, err
	}
	expiresAt := time.Now().UTC().Add(SessionLifetime)

	var session Session
	err = store.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var user User
		err := tx.QueryRow(ctx, `
			INSERT INTO users (first_name, last_name)
			VALUES ($1, $2)
			RETURNING id, first_name, last_name, created_at, updated_at`,
			strings.TrimSpace(params.FirstName),
			strings.TrimSpace(params.LastName),
		).Scan(&user.ID, &user.FirstName, &user.LastName, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert user: %w", err)
		}
		user.Email = email

		_, err = tx.Exec(ctx, `
			INSERT INTO auth_credentials (user_id, email_normalized, password_hash)
			VALUES ($1, $2, $3)`,
			user.ID, email, passwordHash,
		)
		if store.IsUniqueViolation(err, "auth_credentials_email_normalized_unique") {
			return ErrEmailTaken
		}
		if err != nil {
			return fmt.Errorf("insert credentials: %w", err)
		}

		session, err = insertSession(ctx, tx, user, rawToken, tokenHash, expiresAt)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Session{}, err
	}

	s.maybeDeleteExpiredSessions(ctx)
	return session, nil
}

// Login verifies credentials and creates a new session.
func (s *Service) Login(ctx context.Context, email, password string) (Session, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return Session{}, ErrInvalidCredentials
	}

	var user User
	var passwordHash string
	err = s.pool.QueryRow(ctx, `
		SELECT u.id, u.first_name, u.last_name, c.email_normalized, c.password_hash,
		       u.created_at, u.updated_at
		FROM auth_credentials c
		JOIN users u ON u.id = c.user_id
		WHERE c.email_normalized = $1`,
		normalized,
	).Scan(
		&user.ID, &user.FirstName, &user.LastName, &user.Email, &passwordHash,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, fmt.Errorf("look up credentials: %w", err)
	}

	matches, err := VerifyPassword(passwordHash, password)
	if err != nil {
		return Session{}, fmt.Errorf("verify password: %w", err)
	}
	if !matches {
		return Session{}, ErrInvalidCredentials
	}

	session, err := s.createSession(ctx, user)
	if err != nil {
		return Session{}, err
	}
	s.maybeDeleteExpiredSessions(ctx)
	return session, nil
}

// SessionByToken resolves a raw session token to its user.
func (s *Service) SessionByToken(ctx context.Context, rawToken string) (Session, error) {
	if rawToken == "" {
		return Session{}, ErrSessionNotFound
	}

	tokenHash := hashSessionToken(rawToken)
	var session Session
	err := s.pool.QueryRow(ctx, `
		SELECT s.user_id, s.expires_at,
		       u.first_name, u.last_name, c.email_normalized, u.created_at, u.updated_at,
		       s.active_household_id
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		JOIN auth_credentials c ON c.user_id = s.user_id
		WHERE s.token_hash = $1`,
		tokenHash,
	).Scan(
		&session.UserID, &session.ExpiresAt,
		&session.User.FirstName, &session.User.LastName, &session.User.Email,
		&session.User.CreatedAt, &session.User.UpdatedAt,
		&session.ActiveHouseholdID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrSessionNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("look up session: %w", err)
	}
	session.User.ID = session.UserID
	session.TokenHash = tokenHash

	if !time.Now().UTC().Before(session.ExpiresAt) {
		_, _ = s.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", tokenHash)
		return Session{}, ErrSessionNotFound
	}

	return session, nil
}

// DeleteExpiredSessions removes every session whose expiry has passed and
// returns the number removed.
func (s *Service) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE expires_at <= now()")
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// maybeDeleteExpiredSessions runs cleanup at most once per hour per process.
// Failures are logged and never affect the session that triggered them.
func (s *Service) maybeDeleteExpiredSessions(ctx context.Context) {
	now := s.now()
	s.cleanupMu.Lock()
	if !s.lastCleanup.IsZero() && now.Sub(s.lastCleanup) < sessionCleanupInterval {
		s.cleanupMu.Unlock()
		return
	}
	s.lastCleanup = now
	cleanup := s.cleanup
	s.cleanupMu.Unlock()

	if cleanup == nil {
		return
	}
	if _, err := cleanup(ctx); err != nil {
		slog.Warn("delete expired sessions", "error", err)
	}
}

// DeleteSession revokes a session token. It is safe to call for unknown tokens.
func (s *Service) DeleteSession(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", hashSessionToken(rawToken)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// SetActiveHousehold updates the session preference. Passing nil clears the
// active household. The user must belong to the selected household.
func (s *Service) SetActiveHousehold(ctx context.Context, tokenHash string, userID uuid.UUID, householdID *uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE sessions s
		SET active_household_id = $3
		WHERE s.token_hash = $1 AND s.user_id = $2
		  AND ($3::uuid IS NULL OR EXISTS (
		      SELECT 1
		      FROM household_memberships m
		      WHERE m.household_id = $3 AND m.user_id = $2
		  ))`,
		tokenHash, userID, householdID,
	)
	if err != nil {
		return fmt.Errorf("set active household: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	var sessionExists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM sessions WHERE token_hash = $1 AND user_id = $2)`,
		tokenHash, userID,
	).Scan(&sessionExists); err != nil {
		return fmt.Errorf("check session: %w", err)
	}
	if !sessionExists {
		return ErrSessionNotFound
	}
	return ErrHouseholdAccess
}

type queryRower interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func (s *Service) createSession(ctx context.Context, user User) (Session, error) {
	rawToken, tokenHash, err := newSessionToken()
	if err != nil {
		return Session{}, err
	}
	expiresAt := time.Now().UTC().Add(SessionLifetime)
	return insertSession(ctx, s.pool, user, rawToken, tokenHash, expiresAt)
}

func insertSession(
	ctx context.Context,
	query queryRower,
	user User,
	rawToken, tokenHash string,
	expiresAt time.Time,
) (Session, error) {
	if _, err := query.Exec(ctx, `
		INSERT INTO sessions (token_hash, user_id, expires_at)
		VALUES ($1, $2, $3)`,
		tokenHash, user.ID, expiresAt,
	); err != nil {
		return Session{}, fmt.Errorf("insert session: %w", err)
	}

	return Session{
		UserID:    user.ID,
		Token:     rawToken,
		TokenHash: tokenHash,
		User:      user,
		ExpiresAt: expiresAt,
	}, nil
}
