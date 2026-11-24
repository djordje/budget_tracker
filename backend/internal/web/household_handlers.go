package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/households"
)

type newHouseholdPageData struct {
	pageData
	Name         string
	Currencies   []finance.Currency
	CurrencyCode string
	FieldErrors  map[string]string
}

type householdSettingsPageData struct {
	pageData
	Household          households.Household
	Members            []households.Member
	Options            households.Options
	Currencies         []finance.Currency
	Invitations        []invitationRow
	Name               string
	FieldErrors        map[string]string
	OptionsFieldErrors map[string]string
	InviteEmail        string
	InviteRole         string
	InviteFieldErrors  map[string]string
	NewInvitationLink  string
}

// invitationRow is a listed invitation with its presentation state. Expired
// invitations stay listed and are labelled expired.
type invitationRow struct {
	households.Invitation
	Expired bool
}

// NewHouseholdPage serves the household creation form.
func (h *Handler) NewHouseholdPage(w http.ResponseWriter, r *http.Request) {
	data, err := h.newHouseholdData(r, "", nil)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.render(w, r, "household_new", http.StatusOK, data)
}

// CreateHousehold creates a household and makes it active. The HTML form
// requires a visible currency choice instead of silently assuming the default
// that the JSON adapter keeps for clients that omit the field.
func (h *Handler) CreateHousehold(w http.ResponseWriter, r *http.Request) {
	session := sessionFromContext(r)
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}

	name := r.FormValue("name")
	currencyCode := strings.ToUpper(strings.TrimSpace(r.FormValue("currency_code")))
	if currencyCode == "" {
		h.renderNewHousehold(w, r, http.StatusUnprocessableEntity, name, currencyCode, map[string]string{
			"currency_code": "Choose a currency.",
		})
		return
	}

	created, err := h.households.CreateAndActivate(r.Context(), households.CreateParams{
		UserID:           session.User.ID,
		SessionTokenHash: session.TokenHash,
		Name:             name,
		CurrencyCode:     currencyCode,
	})
	if err != nil {
		var validation *households.ValidationError
		switch {
		case errors.As(err, &validation):
			h.renderNewHousehold(w, r, http.StatusUnprocessableEntity, name, currencyCode, map[string]string{
				validation.Field: validation.Message,
			})
		case errors.Is(err, auth.ErrSessionNotFound):
			h.cookies.Clear(w)
			h.renderError(w, r, http.StatusUnauthorized, "Session expired", "Sign in again to create a household.")
		default:
			h.internalError(w, r, err)
		}
		return
	}

	http.Redirect(w, r, householdURL(created.ID, "/entries?notice=household_created"), http.StatusSeeOther)
}

// renderNewHousehold re-renders the creation form with the submitted values
// and field errors. The catalog is always shown so the choice stays available.
func (h *Handler) renderNewHousehold(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	name, currencyCode string,
	fieldErrors map[string]string,
) {
	data, err := h.newHouseholdData(r, name, fieldErrors)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	// Leave the prompt selected unless the submitted code matches an option.
	// Otherwise browsers would silently choose the first enabled currency.
	for _, currency := range data.Currencies {
		if currency.Code == currencyCode {
			data.CurrencyCode = currencyCode
			break
		}
	}
	h.render(w, r, "household_new", status, data)
}

func (h *Handler) newHouseholdData(
	r *http.Request,
	name string,
	fieldErrors map[string]string,
) (newHouseholdPageData, error) {
	currencies, err := h.entries.Currencies(r.Context())
	if err != nil {
		return newHouseholdPageData{}, err
	}
	return newHouseholdPageData{
		pageData:    h.basePageData(r, "New household"),
		Name:        name,
		Currencies:  currencies,
		FieldErrors: fieldErrors,
	}, nil
}

// HouseholdSettings serves household settings for admins.
func (h *Handler) HouseholdSettings(w http.ResponseWriter, r *http.Request) {
	householdID, err := householdIDFromPath(r)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Household not found", "That household is not available.")
		return
	}
	data, err := h.settingsData(r, householdID)
	if err != nil {
		h.renderSettingsError(w, r, err)
		return
	}
	h.render(w, r, "household_settings", http.StatusOK, data)
}

// UpdateHousehold renames a household.
func (h *Handler) UpdateHousehold(w http.ResponseWriter, r *http.Request) {
	householdID, err := householdIDFromPath(r)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Household not found", "That household is not available.")
		return
	}
	session := sessionFromContext(r)
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}
	name := r.FormValue("name")

	_, err = h.households.Rename(r.Context(), session.User.ID, householdID, name)
	if err != nil {
		var validation *households.ValidationError
		switch {
		case errors.As(err, &validation):
			data, dataErr := h.settingsData(r, householdID)
			if dataErr != nil {
				h.renderSettingsError(w, r, dataErr)
				return
			}
			data.Name = name
			data.FieldErrors = map[string]string{validation.Field: validation.Message}
			h.render(w, r, "household_settings", http.StatusUnprocessableEntity, data)
		default:
			h.renderSettingsError(w, r, err)
		}
		return
	}

	http.Redirect(w, r, householdURL(householdID, "/settings?notice=household_saved"), http.StatusSeeOther)
}

// UpdateHouseholdOptions updates household currency defaults.
func (h *Handler) UpdateHouseholdOptions(w http.ResponseWriter, r *http.Request) {
	householdID, err := householdIDFromPath(r)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Household not found", "That household is not available.")
		return
	}
	session := sessionFromContext(r)
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}
	params := households.UpdateOptionsParams{
		DefaultIncomeCurrencyCode:   r.FormValue("default_income_currency_code"),
		DefaultSpendingCurrencyCode: r.FormValue("default_spending_currency_code"),
		DefaultDisplayCurrencyCode:  r.FormValue("default_display_currency_code"),
	}
	if _, err := h.households.UpdateOptions(r.Context(), session.User.ID, householdID, params); err != nil {
		var validation *households.OptionsValidationError
		if errors.As(err, &validation) {
			data, dataErr := h.settingsData(r, householdID)
			if dataErr != nil {
				h.renderSettingsError(w, r, dataErr)
				return
			}
			data.OptionsFieldErrors = validation.Fields
			h.render(w, r, "household_settings", http.StatusUnprocessableEntity, data)
			return
		}
		h.renderSettingsError(w, r, err)
		return
	}
	http.Redirect(w, r, householdURL(householdID, "/settings?notice=options_saved"), http.StatusSeeOther)
}

// CreateHouseholdInvitation creates a shareable invitation link.
func (h *Handler) CreateHouseholdInvitation(w http.ResponseWriter, r *http.Request) {
	householdID, err := householdIDFromPath(r)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Household not found", "That household is not available.")
		return
	}
	session := sessionFromContext(r)
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}
	email := r.FormValue("email")
	role := r.FormValue("role")
	if role == "" {
		role = string(households.RoleMember)
	}
	_, token, err := h.households.CreateInvitation(r.Context(), households.CreateInvitationParams{
		HouseholdID: householdID,
		ActorID:     session.User.ID,
		Email:       email,
		Role:        households.Role(role),
	})
	if err != nil {
		data, dataErr := h.settingsData(r, householdID)
		if dataErr != nil {
			h.renderSettingsError(w, r, dataErr)
			return
		}
		data.InviteEmail = email
		data.InviteRole = role
		var validation *households.ValidationError
		switch {
		case errors.As(err, &validation):
			data.InviteFieldErrors = map[string]string{validation.Field: validation.Message}
		case errors.Is(err, households.ErrInvitationExists):
			data.InviteFieldErrors = map[string]string{"email": "A pending invitation already exists for this email."}
		case errors.Is(err, households.ErrAlreadyMember):
			data.InviteFieldErrors = map[string]string{"email": "That email already belongs to the household."}
		default:
			h.renderSettingsError(w, r, err)
			return
		}
		h.render(w, r, "household_settings", http.StatusUnprocessableEntity, data)
		return
	}

	data, err := h.settingsData(r, householdID)
	if err != nil {
		h.renderSettingsError(w, r, err)
		return
	}
	data.NewInvitationLink = "/invitations/" + token
	data.Notice = "Invitation created. Share the link with the invited person."
	data.InviteEmail = ""
	data.InviteRole = string(households.RoleMember)
	h.render(w, r, "household_settings", http.StatusCreated, data)
}

// RevokeHouseholdInvitation removes a pending invitation.
func (h *Handler) RevokeHouseholdInvitation(w http.ResponseWriter, r *http.Request) {
	householdID, err := householdIDFromPath(r)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Household not found", "That household is not available.")
		return
	}
	session := sessionFromContext(r)
	invitationID, err := uuid.Parse(r.PathValue("invitationID"))
	if err != nil {
		h.renderSettingsError(w, r, households.ErrInvitationNotFound)
		return
	}
	if err := h.households.RevokeInvitation(r.Context(), session.User.ID, householdID, invitationID); err != nil {
		h.renderSettingsError(w, r, err)
		return
	}
	http.Redirect(w, r, householdURL(householdID, "/settings?notice=invitation_revoked"), http.StatusSeeOther)
}

func (h *Handler) settingsData(r *http.Request, householdID uuid.UUID) (householdSettingsPageData, error) {
	data := h.basePageData(r, "Household settings")
	household, err := scopedHousehold(r)
	if err != nil {
		return householdSettingsPageData{}, err
	}
	session := sessionFromContext(r)
	if !household.IsAdmin() {
		return householdSettingsPageData{}, households.ErrForbidden
	}

	members, err := h.households.Members(r.Context(), session.User.ID, householdID)
	if err != nil {
		return householdSettingsPageData{}, err
	}
	options, err := h.households.GetOptions(r.Context(), session.User.ID, householdID)
	if err != nil {
		return householdSettingsPageData{}, err
	}
	currencies, err := h.entries.Currencies(r.Context())
	if err != nil {
		return householdSettingsPageData{}, err
	}
	invitations, err := h.households.ListInvitations(r.Context(), session.User.ID, householdID)
	if err != nil {
		return householdSettingsPageData{}, err
	}
	now := time.Now().UTC()
	invitationRows := make([]invitationRow, 0, len(invitations))
	for _, invitation := range invitations {
		invitationRows = append(invitationRows, invitationRow{
			Invitation: invitation,
			Expired:    invitation.Expired(now),
		})
	}

	return householdSettingsPageData{
		pageData:    data,
		Household:   *household,
		Members:     members,
		Options:     options,
		Currencies:  currencies,
		Invitations: invitationRows,
		Name:        household.Name,
		InviteRole:  string(households.RoleMember),
	}, nil
}

func (h *Handler) renderSettingsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, households.ErrInvitationNotFound):
		h.renderError(w, r, http.StatusNotFound, "Invitation not found", "That invitation is not available.")
	case errors.Is(err, households.ErrInvitationAccepted):
		h.renderError(w, r, http.StatusConflict, "Invitation already accepted", "That invitation is no longer pending.")
	case errors.Is(err, households.ErrNotFound):
		h.renderError(w, r, http.StatusNotFound, "Household not found", "That household is not available.")
	case errors.Is(err, households.ErrForbidden):
		h.renderError(w, r, http.StatusForbidden, "Not allowed", "Only household admins can manage these settings.")
	default:
		h.internalError(w, r, err)
	}
}

func householdIDFromPath(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("householdID"))
	if err != nil {
		return uuid.Nil, errors.New("invalid household ID")
	}
	return id, nil
}
