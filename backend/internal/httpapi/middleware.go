package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"path"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"budgettracker/internal/auth"
)

type requestIDKey struct{}

// RequestID assigns a request identifier to the response and context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the request identifier assigned by RequestID.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func newRequestID() string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(bytes[:])
}

// RequestLogger logs one structured line per HTTP request.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(recorder, r)

			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			logger.Info("http request",
				"request_id", RequestIDFromContext(r.Context()),
				"method", r.Method,
				"path", RedactInvitationPath(r.URL.Path),
				"status", status,
				"bytes", recorder.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}

// RedactInvitationPath returns the request path with any invitation token
// removed. It cleans the path first because the mux routes a cleaned copy, so a
// request such as /x/../invitations/<token> still reaches the invitation
// handler. Directory paths without a token are left unchanged.
func RedactInvitationPath(rawPath string) string {
	cleaned := path.Clean(rawPath)
	if cleaned == "/invitations" || !strings.HasPrefix(cleaned, "/invitations/") {
		return rawPath
	}
	return "/invitations/[redacted]"
}

// Recoverer turns a handler panic into a logged response. Before the response
// is committed, API requests receive the standard JSON error envelope and
// other requests receive a plain-text 500. After commitment it only logs the
// panic so a partial response is never followed by a second status or body.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tracker := &commitTracker{ResponseWriter: w}
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				logger.Error("panic recovered",
					"request_id", RequestIDFromContext(r.Context()),
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				if tracker.committed {
					return
				}
				if strings.HasPrefix(r.URL.Path, "/api/") {
					WriteError(tracker, http.StatusInternalServerError, "internal_error", "The request could not be processed.")
					return
				}
				http.Error(tracker, "internal server error", http.StatusInternalServerError)
			}()

			next.ServeHTTP(tracker, r)
		})
	}
}

type commitTracker struct {
	http.ResponseWriter
	committed bool
}

func (t *commitTracker) WriteHeader(status int) {
	t.committed = true
	t.ResponseWriter.WriteHeader(status)
}

func (t *commitTracker) Write(body []byte) (int, error) {
	t.committed = true
	return t.ResponseWriter.Write(body)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (t *commitTracker) Unwrap() http.ResponseWriter {
	return t.ResponseWriter
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	written, err := r.ResponseWriter.Write(body)
	r.bytes += written
	return written, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// RequireAuthAPI rejects unauthenticated API requests with a JSON 401.
func RequireAuthAPI(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.FromContext(r.Context()); !ok {
			WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
			return
		}
		next(w, r)
	}
}

// maxFormBodyBytes bounds HTML form request bodies, matching the JSON body
// limit so both adapters reject oversized requests instead of reading them.
const maxFormBodyBytes = 1 << 20

// LimitHTMLFormBody bounds HTML form request bodies before anything parses
// them. CSRF protection reads the submitted form value, so this bound has to
// wrap Protect. API paths are skipped because the JSON adapter applies its own
// bound and reports its own too-large error.
func LimitHTMLFormBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if r.ContentLength > maxFormBodyBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
		next.ServeHTTP(w, r)
	})
}
