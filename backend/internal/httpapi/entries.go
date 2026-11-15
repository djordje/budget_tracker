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

// EntryService is the finance behavior needed by JSON handlers.
type EntryService interface {
	Currencies(ctx context.Context) ([]finance.Currency, error)
	Create(ctx context.Context, params finance.CreateParams) (finance.Entry, error)
	Get(ctx context.Context, actorID uuid.UUID, kind finance.Kind, id int64) (finance.Entry, error)
	Update(ctx context.Context, params finance.UpdateParams) (finance.Entry, error)
	Delete(ctx context.Context, actorID uuid.UUID, kind finance.Kind, id int64) error
	List(ctx context.Context, params finance.ListParams) (finance.ListResult, error)
}

// EntryHandler serves income and spending JSON endpoints.
type EntryHandler struct {
	entries EntryService
	logger  *slog.Logger
}

// NewEntryHandler constructs entry JSON handlers.
func NewEntryHandler(entries EntryService, logger *slog.Logger) *EntryHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &EntryHandler{entries: entries, logger: logger}
}

// ListIncome lists income entries for the active household.
func (h *EntryHandler) ListIncome(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, finance.KindIncome)
}

// ListSpending lists spending entries for the active household.
func (h *EntryHandler) ListSpending(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, finance.KindSpending)
}

func (h *EntryHandler) list(w http.ResponseWriter, r *http.Request, kind finance.Kind) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := activeHouseholdID(w, session)
	if !ok {
		return
	}

	scope, err := finance.ParseScope(r.URL.Query().Get("scope"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "Scope must be mine or all.")
		return
	}
	from, err := parseOptionalDate(r.URL.Query().Get("from"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "The from date must use YYYY-MM-DD.")
		return
	}
	to, err := parseOptionalDate(r.URL.Query().Get("to"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "The to date must use YYYY-MM-DD.")
		return
	}
	page, ok := parseOptionalInt(w, r.URL.Query().Get("page"), "page")
	if !ok {
		return
	}
	pageSize, ok := parseOptionalInt(w, r.URL.Query().Get("page_size"), "page_size")
	if !ok {
		return
	}

	result, err := h.entries.List(r.Context(), finance.ListParams{
		Kind:        kind,
		HouseholdID: householdID,
		ActorID:     session.User.ID,
		Scope:       scope,
		From:        from,
		To:          to,
		Page:        page,
		PageSize:    pageSize,
	})
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	items := make([]entryJSON, 0, len(result.Entries))
	for _, entry := range result.Entries {
		items = append(items, toEntryJSON(entry))
	}
	WriteJSON(w, http.StatusOK, entriesResponse{
		Entries: items,
		Pagination: paginationJSON{
			Page:       result.Page,
			PageSize:   result.PageSize,
			Total:      result.Total,
			TotalPages: result.TotalPages,
		},
	})
}

// CreateIncome creates an income entry.
func (h *EntryHandler) CreateIncome(w http.ResponseWriter, r *http.Request) {
	h.create(w, r, finance.KindIncome)
}

// CreateSpending creates a spending entry.
func (h *EntryHandler) CreateSpending(w http.ResponseWriter, r *http.Request) {
	h.create(w, r, finance.KindSpending)
}

func (h *EntryHandler) create(w http.ResponseWriter, r *http.Request, kind finance.Kind) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := activeHouseholdID(w, session)
	if !ok {
		return
	}

	var request entryCreateRequest
	if err := decodeJSON(r, &request); err != nil {
		WriteDecodeError(w, err)
		return
	}
	entryDate, err := parseOptionalDate(request.EntryDate)
	if err != nil {
		WriteValidationError(w, map[string]string{"entry_date": "Use a valid date in YYYY-MM-DD format."})
		return
	}

	var date time.Time
	if entryDate != nil {
		date = *entryDate
	}
	entry, err := h.entries.Create(r.Context(), finance.CreateParams{
		Kind:          kind,
		HouseholdID:   householdID,
		ActorID:       session.User.ID,
		Amount:        request.Amount,
		CurrencyCode:  request.CurrencyCode,
		EntryDate:     date,
		Description:   request.Description,
		HouseholdWide: request.HouseholdWide,
	})
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	w.Header().Set("Location", "/api/v1/"+string(kind)+"s/"+strconv.FormatInt(entry.ID, 10))
	WriteJSON(w, http.StatusCreated, entryResponse{Entry: toEntryJSON(entry)})
}

// GetIncome returns one income entry.
func (h *EntryHandler) GetIncome(w http.ResponseWriter, r *http.Request) {
	h.get(w, r, finance.KindIncome)
}

// GetSpending returns one spending entry.
func (h *EntryHandler) GetSpending(w http.ResponseWriter, r *http.Request) {
	h.get(w, r, finance.KindSpending)
}

func (h *EntryHandler) get(w http.ResponseWriter, r *http.Request, kind finance.Kind) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	id, ok := entryIDFromPath(w, r)
	if !ok {
		return
	}
	entry, err := h.entries.Get(r.Context(), session.User.ID, kind, id)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, entryResponse{Entry: toEntryJSON(entry)})
}

// UpdateIncome updates one income entry.
func (h *EntryHandler) UpdateIncome(w http.ResponseWriter, r *http.Request) {
	h.update(w, r, finance.KindIncome)
}

// UpdateSpending updates one spending entry.
func (h *EntryHandler) UpdateSpending(w http.ResponseWriter, r *http.Request) {
	h.update(w, r, finance.KindSpending)
}

func (h *EntryHandler) update(w http.ResponseWriter, r *http.Request, kind finance.Kind) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	id, ok := entryIDFromPath(w, r)
	if !ok {
		return
	}

	var request entryUpdateRequest
	if err := decodeJSON(r, &request); err != nil {
		WriteDecodeError(w, err)
		return
	}

	var entryDate *time.Time
	if request.EntryDate != nil {
		parsed, err := parseOptionalDate(*request.EntryDate)
		if err != nil || parsed == nil {
			WriteValidationError(w, map[string]string{"entry_date": "Use a valid date in YYYY-MM-DD format."})
			return
		}
		entryDate = parsed
	}

	entry, err := h.entries.Update(r.Context(), finance.UpdateParams{
		Kind:         kind,
		ID:           id,
		ActorID:      session.User.ID,
		Amount:       request.Amount,
		CurrencyCode: request.CurrencyCode,
		EntryDate:    entryDate,
		Description:  request.Description,
	})
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, entryResponse{Entry: toEntryJSON(entry)})
}

// DeleteIncome deletes one income entry.
func (h *EntryHandler) DeleteIncome(w http.ResponseWriter, r *http.Request) {
	h.delete(w, r, finance.KindIncome)
}

// DeleteSpending deletes one spending entry.
func (h *EntryHandler) DeleteSpending(w http.ResponseWriter, r *http.Request) {
	h.delete(w, r, finance.KindSpending)
}

func (h *EntryHandler) delete(w http.ResponseWriter, r *http.Request, kind finance.Kind) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	id, ok := entryIDFromPath(w, r)
	if !ok {
		return
	}
	if err := h.entries.Delete(r.Context(), session.User.ID, kind, id); err != nil {
		h.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Currencies lists supported currencies.
func (h *EntryHandler) Currencies(w http.ResponseWriter, r *http.Request) {
	currencies, err := h.entries.Currencies(r.Context())
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	items := make([]currencyJSON, 0, len(currencies))
	for _, currency := range currencies {
		items = append(items, currencyJSON{
			Code:        currency.Code,
			NumericCode: currency.NumericCode,
			Name:        currency.Name,
			MinorUnits:  currency.MinorUnits,
		})
	}
	WriteJSON(w, http.StatusOK, currenciesResponse{Currencies: items})
}

func (h *EntryHandler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *finance.ValidationError
	switch {
	case errors.As(err, &validation):
		WriteValidationError(w, validation.Fields)
	case errors.Is(err, finance.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "Entry or household not found.")
	case errors.Is(err, finance.ErrForbidden):
		WriteError(w, http.StatusForbidden, "forbidden", "You do not have permission to do that.")
	default:
		h.logger.Error("entry request failed", "path", r.URL.Path, "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "The request could not be processed.")
	}
}

func parseOptionalInt(w http.ResponseWriter, raw, field string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "The "+field+" value must be an integer.")
		return 0, false
	}
	return value, true
}

type entryCreateRequest struct {
	Amount        string `json:"amount"`
	CurrencyCode  string `json:"currency_code"`
	EntryDate     string `json:"entry_date"`
	Description   string `json:"description"`
	HouseholdWide bool   `json:"household_wide"`
}

type entryUpdateRequest struct {
	Amount       *string `json:"amount"`
	CurrencyCode *string `json:"currency_code"`
	EntryDate    *string `json:"entry_date"`
	Description  *string `json:"description"`
}

type entryJSON struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	HouseholdID  string  `json:"household_id"`
	UserID       *string `json:"user_id"`
	AmountMinor  int64   `json:"amount_minor"`
	Amount       string  `json:"amount"`
	CurrencyCode string  `json:"currency_code"`
	EntryDate    string  `json:"entry_date"`
	Description  *string `json:"description"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type entryResponse struct {
	Entry entryJSON `json:"entry"`
}

type entriesResponse struct {
	Entries    []entryJSON    `json:"entries"`
	Pagination paginationJSON `json:"pagination"`
}

type paginationJSON struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type currencyJSON struct {
	Code        string `json:"code"`
	NumericCode int    `json:"numeric_code"`
	Name        string `json:"name"`
	MinorUnits  int32  `json:"minor_units"`
}

type currenciesResponse struct {
	Currencies []currencyJSON `json:"currencies"`
}

func toEntryJSON(entry finance.Entry) entryJSON {
	var userID *string
	if entry.UserID != nil {
		value := entry.UserID.String()
		userID = &value
	}
	return entryJSON{
		ID:           strconv.FormatInt(entry.ID, 10),
		Kind:         string(entry.Kind),
		HouseholdID:  entry.HouseholdID.String(),
		UserID:       userID,
		AmountMinor:  entry.AmountMinor,
		Amount:       entry.AmountString(),
		CurrencyCode: entry.CurrencyCode,
		EntryDate:    entry.DateString(),
		Description:  entry.Description,
		CreatedAt:    entry.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:    entry.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}
