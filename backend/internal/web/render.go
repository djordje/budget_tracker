package web

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
	"budgettracker/internal/httpapi"
)

type pageData struct {
	Title             string
	Year              int
	Authenticated     bool
	User              auth.User
	Households        []households.Household
	ActiveHousehold   *households.Household
	ActiveHouseholdID string
	CSRF              string
	Notice            string
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
	return data
}

func (h *Handler) newPageData(r *http.Request, title string) (pageData, error) {
	data := h.basePageData(r, title)
	session, ok := auth.FromContext(r.Context())
	if !ok {
		return data, nil
	}

	list, err := h.households.List(r.Context(), session.User.ID)
	if err != nil {
		return data, fmt.Errorf("list households: %w", err)
	}
	data.Households = list

	if len(list) == 0 {
		return data, nil
	}

	for i := range list {
		if session.ActiveHouseholdID != nil && list[i].ID == *session.ActiveHouseholdID {
			data.ActiveHousehold = &list[i]
			data.ActiveHouseholdID = list[i].ID.String()
			return data, nil
		}
	}

	data.ActiveHousehold = &list[0]
	data.ActiveHouseholdID = list[0].ID.String()
	if err := h.sessions.SetActiveHousehold(r.Context(), session.TokenHash, session.User.ID, &list[0].ID); err != nil {
		h.logger.Warn("set default active household", "error", err)
	}
	return data, nil
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
	case "invitation_accepted_household_not_selected":
		return "You joined the household. Select it from the household menu to make it active."
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

func (h *Handler) pageDataOrError(w http.ResponseWriter, r *http.Request, title string) (pageData, bool) {
	data, err := h.newPageData(r, title)
	if err != nil {
		h.internalError(w, r, err)
		return pageData{}, false
	}
	return data, true
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, page string, status int, data any) {
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

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Error("request failed", "path", logPath(r), "error", err)
	h.renderError(w, r, http.StatusInternalServerError, "Something went wrong", "Please try again in a moment.")
}

// logPath returns the request path for logging with invitation tokens removed,
// mirroring the redaction in httpapi.RequestLogger.
func logPath(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/invitations/") {
		return "/invitations/[redacted]"
	}
	return r.URL.Path
}
