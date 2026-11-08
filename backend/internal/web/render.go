package web

import (
	"bytes"
	"net/http"

	"budgettracker/internal/auth"
	"budgettracker/internal/httpapi"
)

type pageData struct {
	Title         string
	Year          int
	Authenticated bool
	User          auth.User
	CSRF          string
	Notice        string
}

type errorPageData struct {
	pageData
	Status  int
	Message string
}

func (h *Handler) newPageData(r *http.Request, title string) pageData {
	data := pageData{
		Title:  title,
		Year:   h.year,
		CSRF:   httpapi.CSRFToken(r.Context()),
		Notice: noticeFromQuery(r),
	}
	if session, ok := auth.FromContext(r.Context()); ok {
		data.Authenticated = true
		data.User = session.User
	}
	return data
}

func noticeFromQuery(r *http.Request) string {
	switch r.URL.Query().Get("notice") {
	case "signed_out":
		return "You have been signed out."
	case "signed_in":
		return "You are signed in."
	default:
		return ""
	}
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, page string, status int, data any) {
	tmpl, ok := h.pages[page]
	if !ok {
		h.logger.Error("page template not found", "page", page)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	var body bytes.Buffer
	if err := tmpl.ExecuteTemplate(&body, "layout", data); err != nil {
		h.logger.Error("render page", "page", page, "path", r.URL.Path, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if _, err := body.WriteTo(w); err != nil {
		h.logger.Error("write page", "page", page, "path", r.URL.Path, "error", err)
	}
}

func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	h.render(w, r, "error", status, errorPageData{
		pageData: h.newPageData(r, title),
		Status:   status,
		Message:  message,
	})
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Error("request failed", "path", r.URL.Path, "error", err)
	h.renderError(w, r, http.StatusInternalServerError, "Something went wrong", "Please try again in a moment.")
}
