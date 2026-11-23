package web

import (
	"bytes"
	"net/http"
	"time"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
	"budgettracker/internal/httpapi"
)

type pageData struct {
	Title           string
	Year            int
	Authenticated   bool
	User            auth.User
	Households      []households.Household
	ActiveHousehold *households.Household
	Section         Section
	CurrentPage     Page
	CSRF            string
	Notice          string
}

// SwitchDestination keeps an equivalent list, but never carries filters or
// a resource ID into another household.
func (p pageData) SwitchDestination() string {
	switch p.CurrentPage {
	case PageReports, PageExchangeRates:
		return "/" + string(p.CurrentPage)
	default:
		return "/entries"
	}
}

type errorPageData struct {
	pageData
	Status  int
	Message string
}

func (h *Handler) basePageData(r *http.Request, title string) pageData {
	data := pageData{
		Title:  title,
		Year:   time.Now().Year(),
		CSRF:   httpapi.CSRFToken(r.Context()),
		Notice: noticeFromQuery(r),
	}
	if session, ok := auth.FromContext(r.Context()); ok {
		data.Authenticated = true
		data.User = session.User
	}
	if scope := scopeFromRequest(r); scope.Household != nil {
		data.Households = scope.Memberships
		data.ActiveHousehold = scope.Household
		data.Section = scope.Section
		data.CurrentPage = scope.Page
	}
	return data
}

func findHousehold(items []households.Household, id uuid.UUID) *households.Household {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

func noticeFromQuery(r *http.Request) string {
	switch r.URL.Query().Get("notice") {
	case "signed_out":
		return "You have been signed out."
	case "signed_in":
		return "You are signed in."
	case "household_created":
		return "Household created."
	case "household_saved":
		return "Household settings saved."
	case "entry_created":
		return "Entry added."
	case "entry_updated":
		return "Entry updated."
	case "entry_deleted":
		return "Entry deleted."
	case "rate_created":
		return "Exchange rate added."
	case "rate_updated":
		return "Exchange rate updated."
	case "rate_deleted":
		return "Exchange rate deleted."
	case "invitation_accepted":
		return "You joined the household."
	case "invitation_created":
		return "Invitation created. Share the link with the invited person."
	case "invitation_revoked":
		return "Invitation revoked."
	case "options_saved":
		return "Household currency defaults saved."
	case "preferences_saved":
		return "Preferences saved."
	default:
		return ""
	}
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, page string, status int, data any) {
	if status == http.StatusOK {
		h.rememberHousehold(r)
	}
	h.renderTemplate(w, r, page, "layout", status, data)
}

func (h *Handler) renderFragment(w http.ResponseWriter, r *http.Request, page, name string, status int, data any) {
	h.renderTemplate(w, r, page, name, status, data)
}

// renderTemplate executes one template into a buffer before writing anything,
// so execution failures produce a clean 500 instead of a partial response.
func (h *Handler) renderTemplate(
	w http.ResponseWriter,
	r *http.Request,
	page, name string,
	status int,
	data any,
) {
	tmpl, ok := h.pages[page]
	if !ok {
		h.logger.Error("template page not found", "page", page)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var body bytes.Buffer
	if err := tmpl.ExecuteTemplate(&body, name, data); err != nil {
		h.logger.Error("render template", "page", page, "template", name, "path", logPath(r), "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if _, err := body.WriteTo(w); err != nil {
		h.logger.Error("write template", "page", page, "template", name, "path", logPath(r), "error", err)
	}
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	h.render(w, r, "error", status, errorPageData{
		pageData: h.basePageData(r, title),
		Status:   status,
		Message:  message,
	})
}

// Invalid filter URLs have a full-page error rather than a swappable list.
// Make enhanced submissions show that same error instead of silently ignoring it.
func (h *Handler) renderFilterError(w http.ResponseWriter, r *http.Request, title, message string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", r.URL.RequestURI())
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.renderError(w, r, http.StatusBadRequest, title, message)
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Error("request failed", "path", logPath(r), "error", err)
	h.renderError(w, r, http.StatusInternalServerError, "Something went wrong", "Please try again in a moment.")
}

// logPath returns the request path for logging with invitation tokens removed,
// delegating to the same redaction the HTTP request logger uses.
func logPath(r *http.Request) string {
	return httpapi.RedactInvitationPath(r.URL.Path)
}
