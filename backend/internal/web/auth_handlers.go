package web

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"budgettracker/internal/auth"
)

type loginPageData struct {
	pageData
	Email     string
	Next      string
	FormError string
}

type registerPageData struct {
	pageData
	FirstName   string
	LastName    string
	Email       string
	Next        string
	FieldErrors map[string]string
	FormError   string
}

// Home serves the public landing page.
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	data, ok := h.pageDataOrError(w, r, "Budget Tracker")
	if !ok {
		return
	}
	h.render(w, r, "home", http.StatusOK, data)
}

// LoginPage serves the sign-in form.
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if redirectIfAuthenticated(w, r) {
		return
	}
	h.render(w, r, "login", http.StatusOK, loginPageData{
		pageData: h.basePageData(r, "Sign in"),
		Next:     safeNextPath(r.URL.Query().Get("next"), ""),
	})
}

// Login processes a sign-in form.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if redirectIfAuthenticated(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	// Keep oversized passwords from crossing the service boundary; the service
	// enforces the same bound for every caller.
	if len(password) > auth.PasswordMaxLength {
		h.render(w, r, "login", http.StatusUnprocessableEntity, loginPageData{
			pageData:  h.basePageData(r, "Sign in"),
			Email:     email,
			Next:      safeNextPath(r.FormValue("next"), ""),
			FormError: "Email or password is incorrect.",
		})
		return
	}
	limiterKey := limitKey(r, email)

	if !h.loginLimiter.TryAcquire(limiterKey, clientKey(r)) {
		w.Header().Set("Retry-After", "900")
		h.render(w, r, "login", http.StatusTooManyRequests, loginPageData{
			pageData:  h.basePageData(r, "Sign in"),
			Email:     email,
			Next:      safeNextPath(r.FormValue("next"), ""),
			FormError: "Too many sign-in attempts. Try again in 15 minutes.",
		})
		return
	}

	session, err := h.sessions.Login(r.Context(), email, password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			h.render(w, r, "login", http.StatusUnprocessableEntity, loginPageData{
				pageData:  h.basePageData(r, "Sign in"),
				Email:     email,
				Next:      safeNextPath(r.FormValue("next"), ""),
				FormError: "Email or password is incorrect.",
			})
			return
		}
		h.internalError(w, r, err)
		return
	}

	h.loginLimiter.Reset(limiterKey)
	h.cookies.Set(w, session.Token, session.ExpiresAt)
	http.Redirect(w, r, safeNextPath(r.FormValue("next"), "/app"), http.StatusSeeOther)
}

// RegisterPage serves the account registration form.
func (h *Handler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	if redirectIfAuthenticated(w, r) {
		return
	}
	h.render(w, r, "register", http.StatusOK, registerPageData{
		pageData: h.basePageData(r, "Create account"),
		Next:     safeNextPath(r.URL.Query().Get("next"), ""),
	})
}

// Register processes an account registration form.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if redirectIfAuthenticated(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}

	params := auth.RegisterParams{
		FirstName: r.FormValue("first_name"),
		LastName:  r.FormValue("last_name"),
		Email:     r.FormValue("email"),
		Password:  r.FormValue("password"),
	}
	// Throttle before any service call so no Argon2 work happens for a refused
	// registration attempt.
	if !h.registrationLimiter.TryAcquire(limitKey(r, params.Email), clientKey(r)) {
		w.Header().Set("Retry-After", "900")
		h.render(w, r, "register", http.StatusTooManyRequests, registerPageData{
			pageData:  h.basePageData(r, "Create account"),
			FirstName: params.FirstName,
			LastName:  params.LastName,
			Email:     params.Email,
			Next:      safeNextPath(r.FormValue("next"), ""),
			FormError: "Too many registration attempts. Try again in 15 minutes.",
		})
		return
	}
	fieldErrors := make(map[string]string)
	if params.Password != r.FormValue("password_confirm") {
		fieldErrors["password_confirm"] = "Passwords do not match."
	}

	var session auth.Session
	var err error
	if len(fieldErrors) == 0 {
		session, err = h.sessions.Register(r.Context(), params)
	} else {
		err = &auth.ValidationError{Fields: fieldErrors}
	}

	if err != nil {
		var validation *auth.ValidationError
		switch {
		case errors.As(err, &validation):
			for field, message := range validation.Fields {
				fieldErrors[field] = message
			}
			h.render(w, r, "register", http.StatusUnprocessableEntity, registerPageData{
				pageData:    h.basePageData(r, "Create account"),
				FirstName:   params.FirstName,
				LastName:    params.LastName,
				Email:       params.Email,
				Next:        safeNextPath(r.FormValue("next"), ""),
				FieldErrors: fieldErrors,
			})
		case errors.Is(err, auth.ErrEmailTaken):
			fieldErrors["email"] = "An account with this email already exists."
			h.render(w, r, "register", http.StatusUnprocessableEntity, registerPageData{
				pageData:    h.basePageData(r, "Create account"),
				FirstName:   params.FirstName,
				LastName:    params.LastName,
				Email:       params.Email,
				Next:        safeNextPath(r.FormValue("next"), ""),
				FieldErrors: fieldErrors,
			})
		default:
			h.internalError(w, r, err)
		}
		return
	}

	h.cookies.Set(w, session.Token, session.ExpiresAt)
	http.Redirect(w, r, safeNextPath(r.FormValue("next"), "/app"), http.StatusSeeOther)
}

// Dashboard serves the authenticated application home.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	data, ok := h.pageDataOrError(w, r, "Dashboard")
	if !ok {
		return
	}
	h.render(w, r, "dashboard", http.StatusOK, data)
}

// Logout revokes the current session and clears the cookie.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if rawToken := h.cookies.Token(r); rawToken != "" {
		if err := h.sessions.DeleteSession(r.Context(), rawToken); err != nil {
			h.logger.Error("delete session", "error", err)
		}
	}
	h.cookies.Clear(w)
	http.Redirect(w, r, "/login?notice=signed_out", http.StatusSeeOther)
}

// RequireAuth redirects anonymous visitors to the sign-in page.
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.FromContext(r.Context()); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func redirectIfAuthenticated(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := auth.FromContext(r.Context()); ok {
		http.Redirect(w, r, safeNextPath(r.URL.Query().Get("next"), "/app"), http.StatusSeeOther)
		return true
	}
	return false
}

// limitKey identifies a throttle bucket by client IP and email. Emails that
// normalize are keyed by their canonical form so case and whitespace variants
// share one budget; other input is keyed by its trimmed, lowercased text.
func limitKey(r *http.Request, rawEmail string) string {
	email, err := auth.NormalizeEmail(rawEmail)
	if err != nil {
		email = strings.ToLower(strings.TrimSpace(rawEmail))
	}
	sum := sha256.Sum256([]byte(strings.ToLower(clientHost(r)) + "|" + email))
	return hex.EncodeToString(sum[:])
}

// clientKey identifies the client for limiter admission budgets.
func clientKey(r *http.Request) string {
	sum := sha256.Sum256([]byte(strings.ToLower(clientHost(r))))
	return hex.EncodeToString(sum[:])
}

func clientHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func safeNextPath(raw, fallback string) string {
	value := strings.TrimSpace(raw)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return fallback
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path == "" {
		return fallback
	}
	return parsed.RequestURI()
}
