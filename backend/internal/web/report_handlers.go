package web

import (
	"errors"
	"net/http"
	"time"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/reporting"
)

type reportPageData struct {
	pageData
	Summary    reporting.Summary
	Currencies []finance.Currency
	From       string
	To         string
	Currency   string
	ScopeAll   bool
	IsAdmin    bool
}

// ReportsPage serves the household summary report.
func (h *Handler) ReportsPage(w http.ResponseWriter, r *http.Request) {
	data, err := h.newPageData(r, "Reports")
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if data.ActiveHousehold == nil {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	session, ok := auth.FromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	query := r.URL.Query()
	today := finance.Today()
	defaultFrom := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	fromValue := query.Get("from")
	if fromValue == "" {
		fromValue = defaultFrom.Format(entryDateLayout)
	}
	toValue := query.Get("to")
	if toValue == "" {
		toValue = today.Format(entryDateLayout)
	}
	from, err := time.Parse(entryDateLayout, fromValue)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "Use YYYY-MM-DD for the start date.")
		return
	}
	to, err := time.Parse(entryDateLayout, toValue)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "Use YYYY-MM-DD for the end date.")
		return
	}

	scope, err := finance.ParseScope(query.Get("scope"))
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "Choose my data or the whole household.")
		return
	}
	if !data.ActiveHousehold.IsAdmin() {
		scope = finance.ScopeMine
	}

	currencies, err := h.entries.Currencies(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	view, err := h.households.GetOptionsView(r.Context(), session.User.ID, data.ActiveHousehold.ID)
	if err != nil {
		h.renderReportError(w, r, err)
		return
	}
	currency := query.Get("currency")
	if currency == "" {
		currency = view.Effective.DefaultDisplayCurrencyCode
	}

	summary, err := h.reports.Summary(r.Context(), reporting.SummaryParams{
		HouseholdID:     data.ActiveHousehold.ID,
		ActorID:         session.User.ID,
		Scope:           scope,
		From:            from,
		To:              to,
		DisplayCurrency: currency,
	})
	if err != nil {
		var validation *finance.ValidationError
		if errors.As(err, &validation) {
			h.renderError(w, r, http.StatusBadRequest, "Bad report filters", "Choose a valid date range and display currency.")
			return
		}
		h.renderReportError(w, r, err)
		return
	}

	h.render(w, r, "reports", http.StatusOK, reportPageData{
		pageData:   data,
		Summary:    summary,
		Currencies: currencies,
		From:       fromValue,
		To:         toValue,
		Currency:   summary.DisplayCurrency,
		ScopeAll:   scope == finance.ScopeAll,
		IsAdmin:    data.ActiveHousehold.IsAdmin(),
	})
}

func (h *Handler) renderReportError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, finance.ErrForbidden):
		h.renderError(w, r, http.StatusForbidden, "Not allowed", "You do not have permission to view that report.")
	case errors.Is(err, finance.ErrNotFound):
		h.renderError(w, r, http.StatusNotFound, "Report not found", "That household is not available.")
	default:
		h.internalError(w, r, err)
	}
}
