package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
)

type exchangeRatesPageData struct {
	pageData
	Rates       []finance.ExchangeRate
	Currencies  []finance.Currency
	Form        rateFormValues
	FieldErrors map[string]string
	FormError   string
}

type exchangeRateFormPageData struct {
	pageData
	Action      string
	CancelURL   string
	IsEdit      bool
	From        string
	To          string
	Rate        string
	RateDate    string
	IsMonthly   bool
	Currencies  []finance.Currency
	FieldErrors map[string]string
	FormError   string
}

type rateFormValues struct {
	From      string
	To        string
	Rate      string
	RateDate  string
	IsMonthly bool
}

// ExchangeRatesPage lists rates and shows the add form.
func (h *Handler) ExchangeRatesPage(w http.ResponseWriter, r *http.Request) {
	data, err := h.buildRatesPage(r, rateFormValues{}, nil, "")
	if errors.Is(err, errNoActiveHousehold) {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.render(w, r, "exchange_rates", http.StatusOK, data)
}

// CreateExchangeRate stores a rate from the HTML form.
func (h *Handler) CreateExchangeRate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}
	values := rateFormValuesFromRequest(r)
	rateDate, dateErr := parseFormDate(values.RateDate)
	if dateErr != nil {
		h.renderRateListError(w, r, values, map[string]string{"rate_date": "Use a valid date in YYYY-MM-DD format."}, "")
		return
	}

	session, ok := auth.FromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	data, err := h.newPageData(r, "Exchange rates")
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if data.ActiveHousehold == nil {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}

	_, err = h.rates.CreateExchangeRate(r.Context(), finance.CreateExchangeRateParams{
		HouseholdID:      data.ActiveHousehold.ID,
		ActorID:          session.User.ID,
		FromCurrencyCode: values.From,
		ToCurrencyCode:   values.To,
		Rate:             values.Rate,
		RateDate:         rateDate,
		IsMonthly:        values.IsMonthly,
	})
	if err != nil {
		h.handleRateFormError(w, r, values, err)
		return
	}
	http.Redirect(w, r, "/app/exchange-rates?notice=rate_created", http.StatusSeeOther)
}

// EditExchangeRatePage serves the rate edit form.
func (h *Handler) EditExchangeRatePage(w http.ResponseWriter, r *http.Request) {
	id, err := rateIDFromPath(r)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Rate not found", "That exchange rate is not available.")
		return
	}
	data, err := h.newPageData(r, "Edit exchange rate")
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if data.ActiveHousehold == nil {
		http.Redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	session, _ := auth.FromContext(r.Context())
	rates, err := h.rates.ListExchangeRates(r.Context(), session.User.ID, data.ActiveHousehold.ID)
	if err != nil {
		h.renderRateError(w, r, err)
		return
	}
	rate := findRate(rates, id)
	if rate == nil {
		h.renderError(w, r, http.StatusNotFound, "Rate not found", "That exchange rate is not available.")
		return
	}
	currencies, err := h.entries.Currencies(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.render(w, r, "exchange_rate_form", http.StatusOK, exchangeRateFormPageData{
		pageData:   data,
		Action:     "/app/exchange-rates/" + strconv.FormatInt(rate.ID, 10),
		CancelURL:  "/app/exchange-rates",
		IsEdit:     true,
		From:       rate.FromCurrencyCode,
		To:         rate.ToCurrencyCode,
		Rate:       rate.RateString(),
		RateDate:   rate.RateDate.Format(entryDateLayout),
		IsMonthly:  rate.IsMonthly,
		Currencies: currencies,
	})
}

// UpdateExchangeRate updates a rate from the HTML form.
func (h *Handler) UpdateExchangeRate(w http.ResponseWriter, r *http.Request) {
	id, err := rateIDFromPath(r)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Rate not found", "That exchange rate is not available.")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}
	values := rateFormValuesFromRequest(r)
	rateDate, dateErr := parseFormDate(values.RateDate)
	if dateErr != nil {
		h.renderRateFormError(w, r, id, values, map[string]string{"rate_date": "Use a valid date in YYYY-MM-DD format."}, "")
		return
	}
	session, ok := auth.FromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	from := values.From
	to := values.To
	rate := values.Rate
	monthly := values.IsMonthly
	_, err = h.rates.UpdateExchangeRate(r.Context(), finance.UpdateExchangeRateParams{
		ID:               id,
		ActorID:          session.User.ID,
		FromCurrencyCode: &from,
		ToCurrencyCode:   &to,
		Rate:             &rate,
		RateDate:         &rateDate,
		IsMonthly:        &monthly,
	})
	if err != nil {
		var validation *finance.ValidationError
		switch {
		case errors.As(err, &validation):
			h.renderRateFormError(w, r, id, values, validation.Fields, "")
		case errors.Is(err, finance.ErrRateExists):
			h.renderRateFormError(w, r, id, values, nil, "A rate already exists for that pair and date.")
		default:
			h.renderRateError(w, r, err)
		}
		return
	}
	http.Redirect(w, r, "/app/exchange-rates?notice=rate_updated", http.StatusSeeOther)
}

// DeleteExchangeRate removes a rate from the HTML form.
func (h *Handler) DeleteExchangeRate(w http.ResponseWriter, r *http.Request) {
	id, err := rateIDFromPath(r)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Rate not found", "That exchange rate is not available.")
		return
	}
	session, ok := auth.FromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := h.rates.DeleteExchangeRate(r.Context(), session.User.ID, id); err != nil {
		h.renderRateError(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/exchange-rates?notice=rate_deleted", http.StatusSeeOther)
}

func (h *Handler) buildRatesPage(
	r *http.Request,
	values rateFormValues,
	fieldErrors map[string]string,
	formError string,
) (exchangeRatesPageData, error) {
	data, err := h.newPageData(r, "Exchange rates")
	if err != nil {
		return exchangeRatesPageData{}, err
	}
	if data.ActiveHousehold == nil {
		return exchangeRatesPageData{}, errNoActiveHousehold
	}
	session, ok := auth.FromContext(r.Context())
	if !ok {
		return exchangeRatesPageData{}, auth.ErrSessionNotFound
	}
	rates, err := h.rates.ListExchangeRates(r.Context(), session.User.ID, data.ActiveHousehold.ID)
	if err != nil {
		return exchangeRatesPageData{}, err
	}
	currencies, err := h.entries.Currencies(r.Context())
	if err != nil {
		return exchangeRatesPageData{}, err
	}
	if values.From == "" && values.To == "" {
		values.From, values.To = defaultRatePair(currencies)
	}
	if values.RateDate == "" {
		values.RateDate = time.Now().Format(entryDateLayout)
	}
	if fieldErrors == nil {
		fieldErrors = make(map[string]string)
	}
	return exchangeRatesPageData{
		pageData:    data,
		Rates:       rates,
		Currencies:  currencies,
		Form:        values,
		FieldErrors: fieldErrors,
		FormError:   formError,
	}, nil
}

func (h *Handler) renderRateListError(
	w http.ResponseWriter,
	r *http.Request,
	values rateFormValues,
	fieldErrors map[string]string,
	formError string,
) {
	data, err := h.buildRatesPage(r, values, fieldErrors, formError)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.render(w, r, "exchange_rates", http.StatusUnprocessableEntity, data)
}

func (h *Handler) handleRateFormError(w http.ResponseWriter, r *http.Request, values rateFormValues, err error) {
	var validation *finance.ValidationError
	switch {
	case errors.As(err, &validation):
		h.renderRateListError(w, r, values, validation.Fields, "")
	case errors.Is(err, finance.ErrRateExists):
		h.renderRateListError(w, r, values, nil, "A rate already exists for that pair and date.")
	case errors.Is(err, finance.ErrNotFound):
		h.renderError(w, r, http.StatusNotFound, "Household not found", "That household is not available.")
	default:
		h.internalError(w, r, err)
	}
}

func (h *Handler) renderRateFormError(
	w http.ResponseWriter,
	r *http.Request,
	id int64,
	values rateFormValues,
	fieldErrors map[string]string,
	formError string,
) {
	data, err := h.newPageData(r, "Edit exchange rate")
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	currencies, err := h.entries.Currencies(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if fieldErrors == nil {
		fieldErrors = make(map[string]string)
	}
	h.render(w, r, "exchange_rate_form", http.StatusUnprocessableEntity, exchangeRateFormPageData{
		pageData:    data,
		Action:      "/app/exchange-rates/" + strconv.FormatInt(id, 10),
		CancelURL:   "/app/exchange-rates",
		IsEdit:      true,
		From:        values.From,
		To:          values.To,
		Rate:        values.Rate,
		RateDate:    values.RateDate,
		IsMonthly:   values.IsMonthly,
		Currencies:  currencies,
		FieldErrors: fieldErrors,
		FormError:   formError,
	})
}

func (h *Handler) renderRateError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, finance.ErrNotFound):
		h.renderError(w, r, http.StatusNotFound, "Rate not found", "That exchange rate is not available.")
	default:
		h.internalError(w, r, err)
	}
}

func rateIDFromPath(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("rateID"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid rate ID")
	}
	return id, nil
}

func rateFormValuesFromRequest(r *http.Request) rateFormValues {
	return rateFormValues{
		From:      r.FormValue("from_currency_code"),
		To:        r.FormValue("to_currency_code"),
		Rate:      r.FormValue("rate"),
		RateDate:  r.FormValue("rate_date"),
		IsMonthly: r.FormValue("is_monthly") == "on" || r.FormValue("is_monthly") == "true",
	}
}

func findRate(rates []finance.ExchangeRate, id int64) *finance.ExchangeRate {
	for i := range rates {
		if rates[i].ID == id {
			return &rates[i]
		}
	}
	return nil
}

func defaultRatePair(currencies []finance.Currency) (string, string) {
	has := make(map[string]bool, len(currencies))
	for _, currency := range currencies {
		has[currency.Code] = true
	}
	if has["EUR"] && has["RSD"] {
		return "EUR", "RSD"
	}
	if len(currencies) >= 2 {
		return currencies[0].Code, currencies[1].Code
	}
	return "", ""
}
