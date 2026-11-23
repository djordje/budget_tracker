package web

import (
	"errors"
	"net/http"

	"budgettracker/internal/finance"
	"budgettracker/internal/households"
)

type preferencesPageData struct {
	pageData
	Currencies       []finance.Currency
	HouseholdOptions households.Options
	Effective        households.EffectiveOptions
	IncomeOverride   string
	SpendingOverride string
	DisplayOverride  string
	FieldErrors      map[string]string
}

// PreferencesPage shows personal currency overrides for the active household.
func (h *Handler) PreferencesPage(w http.ResponseWriter, r *http.Request) {
	data, ok := h.preferencesData(w, r, "", "", "", true)
	if !ok {
		return
	}
	h.render(w, r, "preferences", http.StatusOK, data)
}

// UpdatePreferences saves personal currency overrides.
func (h *Handler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	session := sessionFromContext(r)
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}
	income := r.FormValue("default_income_currency_code")
	spending := r.FormValue("default_spending_currency_code")
	display := r.FormValue("default_display_currency_code")

	data, ok := h.preferencesData(w, r, income, spending, display, false)
	if !ok {
		return
	}
	// preferencesData resolves the URL household, so this is never nil.
	householdID := data.ActiveHousehold.ID
	_, err := h.households.SetUserOptions(r.Context(), session.User.ID, householdID, households.UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode:   optionalFormCurrency(income),
		DefaultSpendingCurrencyCode: optionalFormCurrency(spending),
		DefaultDisplayCurrencyCode:  optionalFormCurrency(display),
	})
	if err != nil {
		var validation *households.OptionsValidationError
		if errors.As(err, &validation) {
			data.FieldErrors = validation.Fields
			h.render(w, r, "preferences", http.StatusUnprocessableEntity, data)
			return
		}
		h.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, householdURL(householdID, "/preferences?notice=preferences_saved"), http.StatusSeeOther)
}

func (h *Handler) preferencesData(
	w http.ResponseWriter,
	r *http.Request,
	income, spending, display string,
	loadExisting bool,
) (preferencesPageData, bool) {
	data := h.basePageData(r, "Preferences")
	household, err := scopedHousehold(r)
	if err != nil {
		h.renderPreferencesError(w, r, err)
		return preferencesPageData{}, false
	}
	session := sessionFromContext(r)

	view, err := h.households.GetOptionsView(r.Context(), session.User.ID, household.ID)
	if err != nil {
		h.renderPreferencesError(w, r, err)
		return preferencesPageData{}, false
	}
	currencies, err := h.entries.Currencies(r.Context())
	if err != nil {
		h.internalError(w, r, err)
		return preferencesPageData{}, false
	}

	if loadExisting {
		if income == "" && view.User.DefaultIncomeCurrencyCode != nil {
			income = *view.User.DefaultIncomeCurrencyCode
		}
		if spending == "" && view.User.DefaultSpendingCurrencyCode != nil {
			spending = *view.User.DefaultSpendingCurrencyCode
		}
		if display == "" && view.User.DefaultDisplayCurrencyCode != nil {
			display = *view.User.DefaultDisplayCurrencyCode
		}
	}
	return preferencesPageData{
		pageData:         data,
		Currencies:       currencies,
		HouseholdOptions: view.Household,
		Effective:        view.Effective,
		IncomeOverride:   income,
		SpendingOverride: spending,
		DisplayOverride:  display,
		FieldErrors:      make(map[string]string),
	}, true
}

func (h *Handler) renderPreferencesError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, households.ErrForbidden):
		h.renderError(w, r, http.StatusForbidden, "Not allowed", "You do not have access to those preferences.")
	case errors.Is(err, households.ErrNotFound):
		h.renderError(w, r, http.StatusNotFound, "Preferences not found", "That household is not available.")
	default:
		h.internalError(w, r, err)
	}
}

func optionalFormCurrency(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
