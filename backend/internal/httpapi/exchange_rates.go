package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
)

// ExchangeRateService is the rate behavior needed by JSON handlers.
type ExchangeRateService interface {
	ListExchangeRates(ctx context.Context, actorID, householdID uuid.UUID) ([]finance.ExchangeRate, error)
	CreateExchangeRate(ctx context.Context, params finance.CreateExchangeRateParams) (finance.ExchangeRate, error)
	UpdateExchangeRate(ctx context.Context, params finance.UpdateExchangeRateParams) (finance.ExchangeRate, error)
	DeleteExchangeRate(ctx context.Context, actorID uuid.UUID, rateID int64) error
}

// ExchangeRateHandler serves exchange-rate JSON endpoints.
type ExchangeRateHandler struct {
	rates  ExchangeRateService
	logger *slog.Logger
}

// NewExchangeRateHandler constructs exchange-rate JSON handlers.
func NewExchangeRateHandler(rates ExchangeRateService, logger *slog.Logger) *ExchangeRateHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ExchangeRateHandler{rates: rates, logger: logger}
}

// List returns rates for the active household.
func (h *ExchangeRateHandler) List(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := activeHouseholdID(w, session)
	if !ok {
		return
	}
	rates, err := h.rates.ListExchangeRates(r.Context(), session.User.ID, householdID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	items := make([]exchangeRateJSON, 0, len(rates))
	for _, rate := range rates {
		items = append(items, toExchangeRateJSON(rate))
	}
	WriteJSON(w, http.StatusOK, exchangeRatesResponse{Rates: items})
}

// Create adds a rate to the active household.
func (h *ExchangeRateHandler) Create(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := activeHouseholdID(w, session)
	if !ok {
		return
	}
	var request exchangeRateCreateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		WriteDecodeError(w, err)
		return
	}
	rateDate, err := parseOptionalDate(request.RateDate)
	if err != nil {
		WriteValidationError(w, map[string]string{"rate_date": "Use a valid date in YYYY-MM-DD format."})
		return
	}
	var date time.Time
	if rateDate != nil {
		date = *rateDate
	}
	rate, err := h.rates.CreateExchangeRate(r.Context(), finance.CreateExchangeRateParams{
		HouseholdID:      householdID,
		ActorID:          session.User.ID,
		FromCurrencyCode: request.FromCurrencyCode,
		ToCurrencyCode:   request.ToCurrencyCode,
		Rate:             request.Rate,
		RateDate:         date,
		IsMonthly:        request.IsMonthly,
	})
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/exchange-rates/"+strconv.FormatInt(rate.ID, 10))
	WriteJSON(w, http.StatusCreated, exchangeRateResponse{Rate: toExchangeRateJSON(rate)})
}

// Update changes an existing rate.
func (h *ExchangeRateHandler) Update(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	rateID, ok := rateIDFromPath(w, r)
	if !ok {
		return
	}
	var request exchangeRateUpdateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		WriteDecodeError(w, err)
		return
	}
	var rateDate *time.Time
	if request.RateDate != nil {
		parsed, err := parseOptionalDate(*request.RateDate)
		if err != nil || parsed == nil {
			WriteValidationError(w, map[string]string{"rate_date": "Use a valid date in YYYY-MM-DD format."})
			return
		}
		rateDate = parsed
	}
	rate, err := h.rates.UpdateExchangeRate(r.Context(), finance.UpdateExchangeRateParams{
		ID:               rateID,
		ActorID:          session.User.ID,
		FromCurrencyCode: request.FromCurrencyCode,
		ToCurrencyCode:   request.ToCurrencyCode,
		Rate:             request.Rate,
		RateDate:         rateDate,
		IsMonthly:        request.IsMonthly,
	})
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, exchangeRateResponse{Rate: toExchangeRateJSON(rate)})
}

// Delete removes a rate.
func (h *ExchangeRateHandler) Delete(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	rateID, ok := rateIDFromPath(w, r)
	if !ok {
		return
	}
	if err := h.rates.DeleteExchangeRate(r.Context(), session.User.ID, rateID); err != nil {
		h.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ExchangeRateHandler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *finance.ValidationError
	switch {
	case errors.As(err, &validation):
		WriteValidationError(w, validation.Fields)
	case errors.Is(err, finance.ErrRateExists):
		WriteError(w, http.StatusConflict, "rate_exists", "A rate already exists for that pair and date.")
	case errors.Is(err, finance.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "Exchange rate or household not found.")
	case errors.Is(err, finance.ErrForbidden):
		WriteError(w, http.StatusForbidden, "forbidden", "You do not have permission to do that.")
	default:
		h.logger.Error("exchange rate request failed", "path", r.URL.Path, "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "The request could not be processed.")
	}
}

func rateIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("rateID"), 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusNotFound, "not_found", "Exchange rate not found.")
		return 0, false
	}
	return id, true
}

type exchangeRateCreateRequest struct {
	FromCurrencyCode string `json:"from_currency_code"`
	ToCurrencyCode   string `json:"to_currency_code"`
	Rate             string `json:"rate"`
	RateDate         string `json:"rate_date"`
	IsMonthly        bool   `json:"is_monthly"`
}

type exchangeRateUpdateRequest struct {
	FromCurrencyCode *string `json:"from_currency_code"`
	ToCurrencyCode   *string `json:"to_currency_code"`
	Rate             *string `json:"rate"`
	RateDate         *string `json:"rate_date"`
	IsMonthly        *bool   `json:"is_monthly"`
}

type exchangeRateJSON struct {
	ID               string `json:"id"`
	HouseholdID      string `json:"household_id"`
	FromCurrencyCode string `json:"from_currency_code"`
	ToCurrencyCode   string `json:"to_currency_code"`
	Rate             string `json:"rate"`
	RateDate         string `json:"rate_date"`
	IsMonthly        bool   `json:"is_monthly"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type exchangeRateResponse struct {
	Rate exchangeRateJSON `json:"rate"`
}

type exchangeRatesResponse struct {
	Rates []exchangeRateJSON `json:"rates"`
}

func toExchangeRateJSON(rate finance.ExchangeRate) exchangeRateJSON {
	return exchangeRateJSON{
		ID:               strconv.FormatInt(rate.ID, 10),
		HouseholdID:      rate.HouseholdID.String(),
		FromCurrencyCode: rate.FromCurrencyCode,
		ToCurrencyCode:   rate.ToCurrencyCode,
		Rate:             rate.RateString(),
		RateDate:         rate.RateDate.Format("2006-01-02"),
		IsMonthly:        rate.IsMonthly,
		CreatedAt:        rate.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:        rate.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}
