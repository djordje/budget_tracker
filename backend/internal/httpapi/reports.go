package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/reporting"
)

// SummaryService is the reporting behavior needed by JSON handlers.
type SummaryService interface {
	Summary(ctx context.Context, params reporting.SummaryParams) (reporting.Summary, error)
}

// ReportHandler serves reporting JSON endpoints.
type ReportHandler struct {
	reports SummaryService
	logger  *slog.Logger
}

// NewReportHandler constructs reporting JSON handlers.
func NewReportHandler(reports SummaryService, logger *slog.Logger) *ReportHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReportHandler{reports: reports, logger: logger}
}

// Summary returns converted totals and missing-rate details.
func (h *ReportHandler) Summary(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	query := r.URL.Query()

	householdID, ok := reportHouseholdID(w, r, session)
	if !ok {
		return
	}
	from, ok := requiredReportDate(w, query.Get("from"), "from")
	if !ok {
		return
	}
	to, ok := requiredReportDate(w, query.Get("to"), "to")
	if !ok {
		return
	}
	scope, err := finance.ParseScope(query.Get("scope"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "Scope must be mine or all.")
		return
	}

	summary, err := h.reports.Summary(r.Context(), reporting.SummaryParams{
		HouseholdID:     householdID,
		ActorID:         session.User.ID,
		Scope:           scope,
		From:            from,
		To:              to,
		DisplayCurrency: query.Get("currency"),
	})
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, summaryResponse{Summary: toSummaryJSON(summary)})
}

func (h *ReportHandler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *finance.ValidationError
	switch {
	case errors.As(err, &validation):
		WriteValidationError(w, validation.Fields)
	case errors.Is(err, finance.ErrForbidden):
		WriteError(w, http.StatusForbidden, "forbidden", "You do not have permission to view that report.")
	case errors.Is(err, finance.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "Household not found.")
	default:
		h.logger.Error("report request failed", "path", r.URL.Path, "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "The request could not be processed.")
	}
}

func reportHouseholdID(w http.ResponseWriter, r *http.Request, session auth.Session) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("household_id"))
	if raw == "" {
		return activeHouseholdID(w, session)
	}
	householdID, err := uuid.Parse(raw)
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", "Household not found.")
		return uuid.Nil, false
	}
	return householdID, true
}

func requiredReportDate(w http.ResponseWriter, raw, field string) (time.Time, bool) {
	value, err := parseOptionalDate(strings.TrimSpace(raw))
	if err != nil || value == nil {
		WriteValidationError(w, map[string]string{field: "Use a valid date in YYYY-MM-DD format."})
		return time.Time{}, false
	}
	return *value, true
}

type summaryJSON struct {
	From               string            `json:"from"`
	To                 string            `json:"to"`
	Scope              string            `json:"scope"`
	DisplayCurrency    string            `json:"display_currency"`
	DisplayMinorUnits  int32             `json:"display_minor_units"`
	IncomeMinor        int64             `json:"income_minor"`
	SpendingMinor      int64             `json:"spending_minor"`
	NetMinor           int64             `json:"net_minor"`
	Income             string            `json:"income"`
	Spending           string            `json:"spending"`
	Net                string            `json:"net"`
	EntryCount         int               `json:"entry_count"`
	IncludedEntryCount int               `json:"included_entry_count"`
	MissingEntryCount  int               `json:"missing_entry_count"`
	Complete           bool              `json:"complete"`
	MissingRates       []missingRateJSON `json:"missing_rates"`
}

type missingRateJSON struct {
	FromCurrencyCode string `json:"from_currency_code"`
	ToCurrencyCode   string `json:"to_currency_code"`
	EntryDate        string `json:"entry_date"`
}

type summaryResponse struct {
	Summary summaryJSON `json:"summary"`
}

func toSummaryJSON(summary reporting.Summary) summaryJSON {
	missing := make([]missingRateJSON, 0, len(summary.MissingRates))
	for _, rate := range summary.MissingRates {
		missing = append(missing, missingRateJSON{
			FromCurrencyCode: rate.FromCurrencyCode,
			ToCurrencyCode:   rate.ToCurrencyCode,
			EntryDate:        rate.EntryDate,
		})
	}
	return summaryJSON{
		From:               summary.From,
		To:                 summary.To,
		Scope:              string(summary.Scope),
		DisplayCurrency:    summary.DisplayCurrency,
		DisplayMinorUnits:  summary.DisplayMinorUnits,
		IncomeMinor:        summary.IncomeMinor,
		SpendingMinor:      summary.SpendingMinor,
		NetMinor:           summary.NetMinor,
		Income:             summary.IncomeString(),
		Spending:           summary.SpendingString(),
		Net:                summary.NetString(),
		EntryCount:         summary.EntryCount,
		IncludedEntryCount: summary.IncludedEntryCount,
		MissingEntryCount:  summary.MissingEntryCount,
		Complete:           summary.Complete,
		MissingRates:       missing,
	}
}
