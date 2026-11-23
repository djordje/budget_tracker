package web

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/finance"
	"budgettracker/internal/households"
)

type newHouseholdPageData struct {
	pageData
	Name        string
	FieldErrors map[string]string
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
	h.render(w, r, "household_new", http.StatusOK, newHouseholdPageData{pageData: h.basePageData(r, "New household")})
}

// CreateHousehold creates a household and makes it active.
func (h *Handler) CreateHousehold(w http.ResponseWriter, r *http.Request) {
	session := sessionFromContext(r)
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}

	created, err := h.households.CreateAndActivate(
		r.Context(), session.User.ID, session.TokenHash, r.FormValue("name"),
	)
	if err != nil {
		var validation *households.ValidationError
		switch {
		case errors.As(err, &validation):
			h.render(w, r, "household_new", http.StatusUnprocessableEntity, newHouseholdPageData{
				pageData:    h.basePageData(r, "New household"),
				Name:        r.FormValue("name"),
				FieldErrors: map[string]string{validation.Field: validation.Message},
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

// ActivateHousehold changes the session's active household.
func (h *Handler) ActivateHousehold(w http.ResponseWriter, r *http.Request) {
	session := sessionFromContext(r)
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}

	householdID, err := uuid.Parse(r.FormValue("household_id"))
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Household not found", "That household is not available.")
		return
	}
	if err := h.sessions.SetActiveHousehold(r.Context(), session.TokenHash, session.User.ID, &householdID); err != nil {
		switch {
		case errors.Is(err, auth.ErrHouseholdAccess):
			h.renderError(w, r, http.StatusNotFound, "Household not found", "That household is not available.")
		case errors.Is(err, auth.ErrSessionNotFound):
			h.cookies.Clear(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		default:
			h.internalError(w, r, err)
		}
		return
	}

	http.Redirect(w, r, "/app", http.StatusSeeOther)
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
