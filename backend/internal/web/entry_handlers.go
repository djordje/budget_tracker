package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"budgettracker/internal/finance"
	"budgettracker/internal/households"
)

const entryDateLayout = "2006-01-02"

type entryListPageData struct {
	pageData
	Kind           string
	Entries        []entryRow
	ScopeAll       bool
	IsAdmin        bool
	From           string
	To             string
	Page           int
	Total          int
	TotalPages     int
	IncomeURL      string
	SpendingURL    string
	NewIncomeURL   string
	NewSpendingURL string
	ClearURL       string
	PreviousURL    string
	NextURL        string
}

type entryRow struct {
	Entry       finance.Entry
	KindLabel   string
	IsIncome    bool
	Attribution string
	EditURL     string
	DeleteURL   string
	CanModify   bool
}

type entryFormPageData struct {
	pageData
	KindLabel             string
	Action                string
	CancelURL             string
	IsEdit                bool
	Amount                string
	CurrencyCode          string
	EntryDate             string
	Description           string
	HouseholdWide         bool
	Currencies            []finance.Currency
	IsAdmin               bool
	ShowAttributionChoice bool
	FieldErrors           map[string]string
	FormError             string
}

type entryListErrorData struct {
	pageData
	Message   string
	ReturnURL string
}

type entryFormValues struct {
	Amount        string
	CurrencyCode  string
	EntryDate     string
	Description   string
	HouseholdWide bool
}

// Entries lists the active household's entries.
func (h *Handler) Entries(w http.ResponseWriter, r *http.Request) {
	data := h.basePageData(r, "Entries")

	query := r.URL.Query()
	kind := finance.KindSpending
	switch query.Get("type") {
	case "income":
		kind = finance.KindIncome
	case "", "spending":
	default:
		h.renderFilterError(w, r, "Bad entry filters", "Choose income or spending entries.")
		return
	}

	scope, err := finance.ParseScope(query.Get("scope"))
	if err != nil {
		h.renderFilterError(w, r, "Bad entry filters", "Choose my entries or the whole household.")
		return
	}
	if !data.ActiveHousehold.IsAdmin() {
		scope = finance.ScopeMine
	}

	from, err := finance.ParseOptionalDate(query.Get("from"))
	if err != nil {
		h.renderFilterError(w, r, "Bad entry filters", "Use YYYY-MM-DD for the start date.")
		return
	}
	to, err := finance.ParseOptionalDate(query.Get("to"))
	if err != nil {
		h.renderFilterError(w, r, "Bad entry filters", "Use YYYY-MM-DD for the end date.")
		return
	}
	page, err := parsePage(query.Get("page"))
	if err != nil {
		h.renderFilterError(w, r, "Bad entry filters", "Page must be a positive number.")
		return
	}

	result, err := h.entries.List(r.Context(), finance.ListParams{
		Kind:        kind,
		HouseholdID: data.ActiveHousehold.ID,
		ActorID:     data.User.ID,
		Scope:       scope,
		From:        from,
		To:          to,
		Page:        page,
		PageSize:    20,
	})
	if err != nil {
		var validation *finance.ValidationError
		if errors.As(err, &validation) {
			h.renderFilterError(w, r, "Bad entry filters", "Choose a valid date range and page number.")
			return
		}
		h.renderEntryError(w, r, err)
		return
	}

	rows := make([]entryRow, 0, len(result.Entries))
	for _, entry := range result.Entries {
		rows = append(rows, h.toEntryRow(entry, data.User.ID, data.ActiveHousehold.IsAdmin()))
	}

	fromValue, toValue := "", ""
	if from != nil {
		fromValue = from.Format(entryDateLayout)
	}
	if to != nil {
		toValue = to.Format(entryDateLayout)
	}
	baseURL := func(kind finance.Kind, page int) string {
		return entryListURL(data.ActiveHousehold.ID, kind, scope, fromValue, toValue, page)
	}

	pageData := entryListPageData{
		pageData:       data,
		Kind:           string(kind),
		Entries:        rows,
		ScopeAll:       scope == finance.ScopeAll,
		IsAdmin:        data.ActiveHousehold.IsAdmin(),
		From:           fromValue,
		To:             toValue,
		Page:           result.Page,
		Total:          result.Total,
		TotalPages:     result.TotalPages,
		IncomeURL:      baseURL(finance.KindIncome, 1),
		SpendingURL:    baseURL(finance.KindSpending, 1),
		NewIncomeURL:   householdURL(data.ActiveHousehold.ID, "/incomes/new"),
		NewSpendingURL: householdURL(data.ActiveHousehold.ID, "/spendings/new"),
		ClearURL:       entryListURL(data.ActiveHousehold.ID, kind, scope, "", "", 1),
	}
	if result.Page > 1 {
		pageData.PreviousURL = baseURL(kind, result.Page-1)
	}
	if result.Page < result.TotalPages {
		pageData.NextURL = baseURL(kind, result.Page+1)
	}
	h.render(w, r, "entries", http.StatusOK, pageData)
}

// NewIncomePage serves the income creation form.
func (h *Handler) NewIncomePage(w http.ResponseWriter, r *http.Request) {
	h.newEntry(w, r, finance.KindIncome)
}

// NewSpendingPage serves the spending creation form.
func (h *Handler) NewSpendingPage(w http.ResponseWriter, r *http.Request) {
	h.newEntry(w, r, finance.KindSpending)
}

func (h *Handler) newEntry(w http.ResponseWriter, r *http.Request, kind finance.Kind) {
	data, err := h.entryFormBase(r, kind, false)
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	session := sessionFromContext(r)
	view, err := h.households.GetOptionsView(r.Context(), session.User.ID, data.ActiveHousehold.ID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	data.Amount = ""
	data.CurrencyCode = defaultCurrency(view.Effective, kind)
	data.EntryDate = finance.Today().Format(entryDateLayout)
	data.Description = ""
	data.HouseholdWide = false
	data.ShowAttributionChoice = data.IsAdmin
	data, err = h.orderCurrencyChoices(r.Context(), data)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.render(w, r, "entry_form", http.StatusOK, data)
}

// CreateIncome creates an income from the HTML form.
func (h *Handler) CreateIncome(w http.ResponseWriter, r *http.Request) {
	h.createEntry(w, r, finance.KindIncome)
}

// CreateSpending creates a spending from the HTML form.
func (h *Handler) CreateSpending(w http.ResponseWriter, r *http.Request) {
	h.createEntry(w, r, finance.KindSpending)
}

func (h *Handler) createEntry(w http.ResponseWriter, r *http.Request, kind finance.Kind) {
	session := sessionFromContext(r)
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}

	data, err := h.entryFormBase(r, kind, false)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	values := entryFormValuesFromRequest(r)
	data = applyEntryFormValues(data, values)
	data.ShowAttributionChoice = data.IsAdmin
	data, err = h.orderCurrencyChoices(r.Context(), data)
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	entryDate, dateErr := parseFormDate(values.EntryDate)
	if dateErr != nil {
		data.FieldErrors = map[string]string{"entry_date": "Use a valid date in YYYY-MM-DD format."}
		h.renderEntryForm(w, r, http.StatusUnprocessableEntity, data)
		return
	}

	_, err = h.entries.Create(r.Context(), finance.CreateParams{
		Kind:          kind,
		HouseholdID:   data.ActiveHousehold.ID,
		ActorID:       session.User.ID,
		Amount:        values.Amount,
		CurrencyCode:  values.CurrencyCode,
		EntryDate:     entryDate,
		Description:   values.Description,
		HouseholdWide: values.HouseholdWide,
	})
	if err != nil {
		h.renderEntryFormError(w, r, data, err)
		return
	}

	h.redirectEntryList(w, r, kind, "entry_created")
}

// EditIncomePage serves the income edit form.
func (h *Handler) EditIncomePage(w http.ResponseWriter, r *http.Request) {
	h.editEntry(w, r, finance.KindIncome)
}

// EditSpendingPage serves the spending edit form.
func (h *Handler) EditSpendingPage(w http.ResponseWriter, r *http.Request) {
	h.editEntry(w, r, finance.KindSpending)
}

func (h *Handler) editEntry(w http.ResponseWriter, r *http.Request, kind finance.Kind) {
	session := sessionFromContext(r)
	id, err := entryIDFromPath(r)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Entry not found", "That entry is not available.")
		return
	}
	data, err := h.entryFormBase(r, kind, true)
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	entry, err := h.entries.Get(r.Context(), session.User.ID, kind, id)
	if err != nil {
		h.renderEntryError(w, r, err)
		return
	}
	if entry.HouseholdID != data.ActiveHousehold.ID {
		h.renderError(w, r, http.StatusNotFound, "Entry not found", "That entry is not available.")
		return
	}
	policy := finance.NewEntryPolicy(session.User.ID, data.ActiveHousehold.IsAdmin())
	if !policy.CanModify(entry) {
		h.renderError(w, r, http.StatusForbidden, "Not allowed", "You cannot edit that entry.")
		return
	}
	data.Action = entryURL(data.ActiveHousehold.ID, kind, id)
	data.IsEdit = true
	data.Amount = entry.AmountString()
	data.CurrencyCode = entry.CurrencyCode
	data.EntryDate = entry.DateString()
	if entry.Description != nil {
		data.Description = *entry.Description
	}
	data.HouseholdWide = entry.IsHouseholdWide()
	data.ShowAttributionChoice = false
	data, err = h.orderCurrencyChoices(r.Context(), data)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.render(w, r, "entry_form", http.StatusOK, data)
}

// UpdateIncome updates an income from the HTML form.
func (h *Handler) UpdateIncome(w http.ResponseWriter, r *http.Request) {
	h.updateEntry(w, r, finance.KindIncome)
}

// UpdateSpending updates a spending from the HTML form.
func (h *Handler) UpdateSpending(w http.ResponseWriter, r *http.Request) {
	h.updateEntry(w, r, finance.KindSpending)
}

func (h *Handler) updateEntry(w http.ResponseWriter, r *http.Request, kind finance.Kind) {
	session := sessionFromContext(r)
	id, err := entryIDFromPath(r)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Entry not found", "That entry is not available.")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}

	data, err := h.entryFormBase(r, kind, true)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	entry, err := h.entries.Get(r.Context(), session.User.ID, kind, id)
	if err != nil {
		h.renderEntryError(w, r, err)
		return
	}
	if entry.HouseholdID != data.ActiveHousehold.ID {
		h.renderError(w, r, http.StatusNotFound, "Entry not found", "That entry is not available.")
		return
	}
	values := entryFormValuesFromRequest(r)
	data.Action = entryURL(data.ActiveHousehold.ID, kind, id)
	data.IsEdit = true
	data = applyEntryFormValues(data, values)
	data.HouseholdWide = entry.IsHouseholdWide()
	data.ShowAttributionChoice = false
	data, err = h.orderCurrencyChoices(r.Context(), data)
	if err != nil {
		h.internalError(w, r, err)
		return
	}

	entryDate, dateErr := parseFormDate(values.EntryDate)
	if dateErr != nil {
		data.FieldErrors = map[string]string{"entry_date": "Use a valid date in YYYY-MM-DD format."}
		h.renderEntryForm(w, r, http.StatusUnprocessableEntity, data)
		return
	}
	amount := values.Amount
	currencyCode := values.CurrencyCode
	description := values.Description
	_, err = h.entries.Update(r.Context(), finance.UpdateParams{
		Kind:         kind,
		ID:           id,
		ActorID:      session.User.ID,
		Amount:       &amount,
		CurrencyCode: &currencyCode,
		EntryDate:    &entryDate,
		Description:  &description,
	})
	if err != nil {
		h.renderEntryFormError(w, r, data, err)
		return
	}

	h.redirectEntryList(w, r, kind, "entry_updated")
}

// DeleteIncome deletes an income from the HTML form.
func (h *Handler) DeleteIncome(w http.ResponseWriter, r *http.Request) {
	h.deleteEntry(w, r, finance.KindIncome)
}

// DeleteSpending deletes a spending from the HTML form.
func (h *Handler) DeleteSpending(w http.ResponseWriter, r *http.Request) {
	h.deleteEntry(w, r, finance.KindSpending)
}

func (h *Handler) deleteEntry(w http.ResponseWriter, r *http.Request, kind finance.Kind) {
	session := sessionFromContext(r)
	id, err := entryIDFromPath(r)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Entry not found", "That entry is not available.")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}
	data := h.basePageData(r, "Entries")
	entry, err := h.entries.Get(r.Context(), session.User.ID, kind, id)
	if err != nil {
		h.renderEntryDeleteFailure(w, r, kind, err)
		return
	}
	if entry.HouseholdID != data.ActiveHousehold.ID {
		h.renderEntryDeleteFailure(w, r, kind, finance.ErrNotFound)
		return
	}
	if err := h.entries.Delete(r.Context(), session.User.ID, kind, id); err != nil {
		h.renderEntryDeleteFailure(w, r, kind, err)
		return
	}
	redirectToList(w, r, entryListReturnURL(r, kind)+"&notice=entry_deleted")
}

// renderEntryDeleteFailure reports a failed delete in the format the request
// expects: an HTMX fragment or a full page, as the edit and update handlers do.
func (h *Handler) renderEntryDeleteFailure(w http.ResponseWriter, r *http.Request, kind finance.Kind, err error) {
	if isHTMX(r) {
		h.renderEntryDeleteError(w, r, kind, err)
		return
	}
	h.renderEntryError(w, r, err)
}

func (h *Handler) renderEntryDeleteError(w http.ResponseWriter, r *http.Request, kind finance.Kind, err error) {
	data := entryListErrorData{
		pageData:  h.basePageData(r, "Entries"),
		ReturnURL: entryListReturnURL(r, kind),
	}
	// HTMX only swaps successful responses, so the fragment carries the
	// message with 200 as renderEntryForm does. Non-HTMX requests keep the
	// real status through renderEntryError.
	switch {
	case errors.Is(err, finance.ErrNotFound):
		data.Message = "That entry is no longer available."
		h.renderFragment(w, r, "entries", "entries_error", http.StatusOK, data)
	case errors.Is(err, finance.ErrForbidden):
		data.Message = "You do not have permission to delete that entry."
		h.renderFragment(w, r, "entries", "entries_error", http.StatusOK, data)
	default:
		h.logger.Error("delete entry failed", "path", logPath(r), "error", err)
		data.Message = "The entry could not be deleted. Try again."
		h.renderFragment(w, r, "entries", "entries_error", http.StatusOK, data)
	}
}

func (h *Handler) entryFormBase(r *http.Request, kind finance.Kind, isEdit bool) (entryFormPageData, error) {
	title := "New " + strings.ToLower(kindLabel(kind))
	if isEdit {
		title = "Edit " + strings.ToLower(kindLabel(kind))
	}
	data := h.basePageData(r, title)
	household, err := scopedHousehold(r)
	if err != nil {
		return entryFormPageData{}, err
	}
	currencies, err := h.entries.Currencies(r.Context())
	if err != nil {
		return entryFormPageData{}, err
	}
	return entryFormPageData{
		pageData:    data,
		KindLabel:   kindLabel(kind),
		Action:      householdURL(household.ID, "/"+kindPlural(kind)),
		CancelURL:   entryListURL(household.ID, kind, finance.ScopeMine, "", "", 1),
		Currencies:  currencies,
		IsAdmin:     household.IsAdmin(),
		FieldErrors: make(map[string]string),
	}, nil
}

// orderCurrencyChoices puts the current selection first, then the currencies
// the actor recorded recently in the active household, then the rest of the
// catalog. It runs only when an entry form is rendered, including validation
// responses, and never changes which currency is selected.
func (h *Handler) orderCurrencyChoices(ctx context.Context, data entryFormPageData) (entryFormPageData, error) {
	recent, err := h.entries.RecentCurrencies(ctx, data.ActiveHousehold.ID, data.User.ID)
	if err != nil {
		return data, err
	}
	data.Currencies = finance.OrderCurrencies(data.Currencies, data.CurrencyCode, recent)
	return data, nil
}

func (h *Handler) renderEntryFormError(w http.ResponseWriter, r *http.Request, data entryFormPageData, err error) {
	var validation *finance.ValidationError
	switch {
	case errors.As(err, &validation):
		for field, message := range validation.Fields {
			data.FieldErrors[field] = message
		}
		h.renderEntryForm(w, r, http.StatusUnprocessableEntity, data)
	case errors.Is(err, finance.ErrForbidden):
		data.FormError = "You do not have permission to save this entry."
		h.renderEntryForm(w, r, http.StatusForbidden, data)
	case errors.Is(err, finance.ErrNotFound):
		h.renderError(w, r, http.StatusNotFound, "Entry not found", "That entry is not available.")
	default:
		h.internalError(w, r, err)
	}
}

func (h *Handler) renderEntryForm(w http.ResponseWriter, r *http.Request, status int, data entryFormPageData) {
	if isHTMX(r) {
		h.renderFragment(w, r, "entry_form", "entry_form_panel", http.StatusOK, data)
		return
	}
	h.render(w, r, "entry_form", status, data)
}

func (h *Handler) redirectEntryList(w http.ResponseWriter, r *http.Request, kind finance.Kind, notice string) {
	redirectToList(w, r, entryListURL(scopeFromRequest(r).Household.ID, kind, finance.ScopeMine, "", "", 1)+"&notice="+notice)
}

// redirectToList navigates to target. HTMX callers get HX-Redirect so they
// follow it as a full page load: swapping only the list would leave the layout
// notices, which sit outside the swapped region, unrendered.
func redirectToList(w http.ResponseWriter, r *http.Request, target string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func entryListReturnURL(r *http.Request, kind finance.Kind) string {
	scope := finance.ScopeMine
	if r.FormValue("return_scope") == "all" {
		scope = finance.ScopeAll
	}
	from := r.FormValue("return_from")
	if _, err := finance.ParseOptionalDate(from); err != nil {
		from = ""
	}
	to := r.FormValue("return_to")
	if _, err := finance.ParseOptionalDate(to); err != nil {
		to = ""
	}
	page, err := parsePage(r.FormValue("return_page"))
	if err != nil {
		page = 1
	}
	return entryListURL(scopeFromRequest(r).Household.ID, kind, scope, from, to, page)
}

func (h *Handler) renderEntryError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, finance.ErrNotFound):
		h.renderError(w, r, http.StatusNotFound, "Entry not found", "That entry is not available.")
	case errors.Is(err, finance.ErrForbidden):
		h.renderError(w, r, http.StatusForbidden, "Not allowed", "You do not have permission to do that.")
	default:
		h.internalError(w, r, err)
	}
}

func (h *Handler) toEntryRow(entry finance.Entry, actorID uuid.UUID, isAdmin bool) entryRow {
	attribution := "You"
	if entry.IsHouseholdWide() {
		attribution = "Household"
	} else if entry.UserID == nil || *entry.UserID != actorID {
		attribution = strings.TrimSpace(entry.OwnerFirstName + " " + entry.OwnerLastName)
		if attribution == "" {
			attribution = "Member"
		}
	}
	return entryRow{
		Entry:       entry,
		KindLabel:   kindLabel(entry.Kind),
		IsIncome:    entry.Kind == finance.KindIncome,
		Attribution: attribution,
		EditURL:     entryURL(entry.HouseholdID, entry.Kind, entry.ID) + "/edit",
		DeleteURL:   entryURL(entry.HouseholdID, entry.Kind, entry.ID) + "/delete",
		CanModify:   finance.NewEntryPolicy(actorID, isAdmin).CanModify(entry),
	}
}

func entryIDFromPath(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("entryID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid entry ID")
	}
	return id, nil
}

func entryFormValuesFromRequest(r *http.Request) entryFormValues {
	return entryFormValues{
		Amount:        r.FormValue("amount"),
		CurrencyCode:  r.FormValue("currency_code"),
		EntryDate:     r.FormValue("entry_date"),
		Description:   r.FormValue("description"),
		HouseholdWide: r.FormValue("attribution") == "household",
	}
}

func applyEntryFormValues(data entryFormPageData, values entryFormValues) entryFormPageData {
	data.Amount = values.Amount
	data.CurrencyCode = values.CurrencyCode
	data.EntryDate = values.EntryDate
	data.Description = values.Description
	data.HouseholdWide = values.HouseholdWide
	return data
}

func parseFormDate(raw string) (time.Time, error) {
	parsed, err := finance.ParseOptionalDate(raw)
	if err != nil {
		return time.Time{}, err
	}
	if parsed == nil {
		return time.Time{}, nil
	}
	return *parsed, nil
}

func parsePage(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 1, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, errors.New("invalid page")
	}
	return value, nil
}

func entryURL(id uuid.UUID, kind finance.Kind, entryID int64) string {
	return householdURL(id, "/"+kindPlural(kind)+"/"+strconv.FormatInt(entryID, 10))
}

func entryListURL(id uuid.UUID, kind finance.Kind, scope finance.Scope, from, to string, page int) string {
	values := url.Values{}
	values.Set("type", string(kind))
	if scope == finance.ScopeAll {
		values.Set("scope", "all")
	}
	if from != "" {
		values.Set("from", from)
	}
	if to != "" {
		values.Set("to", to)
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	return householdURL(id, "/entries?"+values.Encode())
}

func kindLabel(kind finance.Kind) string {
	if kind == finance.KindIncome {
		return "Income"
	}
	return "Spending"
}

func kindPlural(kind finance.Kind) string {
	if kind == finance.KindIncome {
		return "incomes"
	}
	return "spendings"
}

func defaultCurrency(options households.EffectiveOptions, kind finance.Kind) string {
	if kind == finance.KindIncome {
		return options.DefaultIncomeCurrencyCode
	}
	return options.DefaultSpendingCurrencyCode
}
