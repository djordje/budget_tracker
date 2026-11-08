package web

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
)

type newHouseholdPageData struct {
	pageData
	Name        string
	FieldErrors map[string]string
}

type householdSettingsPageData struct {
	pageData
	Household   households.Household
	Members     []households.Member
	Options     households.Options
	Name        string
	FieldErrors map[string]string
}

// NewHouseholdPage serves the household creation form.
func (h *Handler) NewHouseholdPage(w http.ResponseWriter, r *http.Request) {
	data, ok := h.pageDataOrError(w, r, "New household")
	if !ok {
		return
	}
	h.render(w, r, "household_new", http.StatusOK, newHouseholdPageData{pageData: data})
}

// CreateHousehold creates a household and makes it active.
func (h *Handler) CreateHousehold(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}

	_, err := h.households.CreateAndActivate(
		r.Context(), session.User.ID, session.TokenHash, r.FormValue("name"),
	)
	if err != nil {
		var validation *households.ValidationError
		switch {
		case errors.As(err, &validation):
			data, ok := h.pageDataOrError(w, r, "New household")
			if !ok {
				return
			}
			h.render(w, r, "household_new", http.StatusUnprocessableEntity, newHouseholdPageData{
				pageData:    data,
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

	http.Redirect(w, r, "/app?notice=household_created", http.StatusSeeOther)
}

// HouseholdSettings serves household settings for admins.
func (h *Handler) HouseholdSettings(w http.ResponseWriter, r *http.Request) {
	householdID, ok := householdIDFromPath(w, r)
	if !ok {
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
	householdID, ok := householdIDFromPath(w, r)
	if !ok {
		return
	}
	session, ok := auth.FromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Bad request", "The form could not be read.")
		return
	}
	name := r.FormValue("name")

	_, err := h.households.Rename(r.Context(), session.User.ID, householdID, name)
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

	http.Redirect(w, r, "/app/households/"+householdID.String()+"/settings?notice=household_saved", http.StatusSeeOther)
}

// ActivateHousehold changes the session's active household.
func (h *Handler) ActivateHousehold(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
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

func (h *Handler) settingsData(r *http.Request, householdID uuid.UUID) (householdSettingsPageData, error) {
	data, err := h.newPageData(r, "Household settings")
	if err != nil {
		return householdSettingsPageData{}, err
	}
	session, ok := auth.FromContext(r.Context())
	if !ok {
		return householdSettingsPageData{}, auth.ErrSessionNotFound
	}

	household := findHousehold(data.Households, householdID)
	if household == nil {
		return householdSettingsPageData{}, households.ErrNotFound
	}
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

	return householdSettingsPageData{
		pageData:  data,
		Household: *household,
		Members:   members,
		Options:   options,
		Name:      household.Name,
	}, nil
}

func (h *Handler) renderSettingsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, households.ErrNotFound), errors.Is(err, auth.ErrSessionNotFound):
		h.renderError(w, r, http.StatusNotFound, "Household not found", "That household is not available.")
	case errors.Is(err, households.ErrForbidden):
		h.renderError(w, r, http.StatusForbidden, "Not allowed", "Only household admins can manage these settings.")
	default:
		h.internalError(w, r, err)
	}
}

func householdIDFromPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("householdID"))
	if err != nil {
		// householdIDFromPath has no handler receiver, so callers render their
		// own not-found response after this returns false.
		http.Error(w, "not found", http.StatusNotFound)
		return uuid.Nil, false
	}
	return id, true
}
