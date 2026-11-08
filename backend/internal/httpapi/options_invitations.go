package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
)

// UpdateOptions updates household currency defaults.
func (h *HouseholdHandler) UpdateOptions(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := householdIDFromPath(w, r)
	if !ok {
		return
	}
	var request updateHouseholdOptionsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		WriteDecodeError(w, err)
		return
	}
	options, err := h.households.UpdateOptions(r.Context(), session.User.ID, householdID, households.UpdateOptionsParams{
		DefaultIncomeCurrencyCode:   request.DefaultIncomeCurrencyCode,
		DefaultSpendingCurrencyCode: request.DefaultSpendingCurrencyCode,
		DefaultDisplayCurrencyCode:  request.DefaultDisplayCurrencyCode,
	})
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, optionsResponse{Options: optionsJSON{
		DefaultIncomeCurrencyCode:   options.DefaultIncomeCurrencyCode,
		DefaultSpendingCurrencyCode: options.DefaultSpendingCurrencyCode,
		DefaultDisplayCurrencyCode:  options.DefaultDisplayCurrencyCode,
	}})
}

// MyOptions returns personal overrides and resolved effective values.
func (h *HouseholdHandler) MyOptions(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := householdIDFromPath(w, r)
	if !ok {
		return
	}
	view, err := h.households.GetOptionsView(r.Context(), session.User.ID, householdID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, myOptionsResponse{
		Options:   toUserOptionsJSON(view.User),
		Effective: toEffectiveOptionsJSON(view.Effective),
	})
}

// UpdateMyOptions replaces personal overrides.
func (h *HouseholdHandler) UpdateMyOptions(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := householdIDFromPath(w, r)
	if !ok {
		return
	}
	var request updateMyOptionsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		WriteDecodeError(w, err)
		return
	}
	view, err := h.households.SetUserOptions(r.Context(), session.User.ID, householdID, households.UpdateUserOptionsParams{
		DefaultIncomeCurrencyCode:   request.DefaultIncomeCurrencyCode,
		DefaultSpendingCurrencyCode: request.DefaultSpendingCurrencyCode,
		DefaultDisplayCurrencyCode:  request.DefaultDisplayCurrencyCode,
	})
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, myOptionsResponse{
		Options:   toUserOptionsJSON(view.User),
		Effective: toEffectiveOptionsJSON(view.Effective),
	})
}

// CreateInvitation invites an email to the household.
func (h *HouseholdHandler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := householdIDFromPath(w, r)
	if !ok {
		return
	}
	var request createInvitationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		WriteDecodeError(w, err)
		return
	}
	invitation, token, err := h.households.CreateInvitation(r.Context(), households.CreateInvitationParams{
		HouseholdID: householdID,
		ActorID:     session.User.ID,
		Email:       request.Email,
		Role:        households.Role(request.Role),
	})
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusCreated, invitationCreatedResponse{
		Invitation: toInvitationJSON(invitation),
		Token:      token,
	})
}

// ListInvitations lists pending invitations for admins.
func (h *HouseholdHandler) ListInvitations(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := householdIDFromPath(w, r)
	if !ok {
		return
	}
	invitations, err := h.households.ListInvitations(r.Context(), session.User.ID, householdID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	items := make([]invitationJSON, 0, len(invitations))
	for _, invitation := range invitations {
		items = append(items, toInvitationJSON(invitation))
	}
	WriteJSON(w, http.StatusOK, invitationsResponse{Invitations: items})
}

// RevokeInvitation removes a pending invitation.
func (h *HouseholdHandler) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := householdIDFromPath(w, r)
	if !ok {
		return
	}
	invitationID, err := uuid.Parse(r.PathValue("invitationID"))
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", "Invitation not found.")
		return
	}
	if err := h.households.RevokeInvitation(r.Context(), session.User.ID, householdID, invitationID); err != nil {
		h.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type updateHouseholdOptionsRequest struct {
	DefaultIncomeCurrencyCode   string `json:"default_income_currency_code"`
	DefaultSpendingCurrencyCode string `json:"default_spending_currency_code"`
	DefaultDisplayCurrencyCode  string `json:"default_display_currency_code"`
}

type updateMyOptionsRequest struct {
	DefaultIncomeCurrencyCode   *string `json:"default_income_currency_code"`
	DefaultSpendingCurrencyCode *string `json:"default_spending_currency_code"`
	DefaultDisplayCurrencyCode  *string `json:"default_display_currency_code"`
}

type userOptionsJSON struct {
	DefaultIncomeCurrencyCode   *string `json:"default_income_currency_code"`
	DefaultSpendingCurrencyCode *string `json:"default_spending_currency_code"`
	DefaultDisplayCurrencyCode  *string `json:"default_display_currency_code"`
}

type effectiveOptionsJSON struct {
	DefaultIncomeCurrencyCode   string `json:"default_income_currency_code"`
	DefaultSpendingCurrencyCode string `json:"default_spending_currency_code"`
	DefaultDisplayCurrencyCode  string `json:"default_display_currency_code"`
}

type myOptionsResponse struct {
	Options   userOptionsJSON      `json:"options"`
	Effective effectiveOptionsJSON `json:"effective"`
}

type createInvitationRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type invitationJSON struct {
	ID              string  `json:"id"`
	HouseholdID     string  `json:"household_id"`
	HouseholdName   string  `json:"household_name"`
	Email           string  `json:"email"`
	Role            string  `json:"role"`
	InvitedByUserID string  `json:"invited_by_user_id"`
	ExpiresAt       string  `json:"expires_at"`
	AcceptedAt      *string `json:"accepted_at"`
	CreatedAt       string  `json:"created_at"`
}

type invitationCreatedResponse struct {
	Invitation invitationJSON `json:"invitation"`
	Token      string         `json:"token"`
}

type invitationsResponse struct {
	Invitations []invitationJSON `json:"invitations"`
}

func toUserOptionsJSON(options households.UserOptions) userOptionsJSON {
	return userOptionsJSON{
		DefaultIncomeCurrencyCode:   options.DefaultIncomeCurrencyCode,
		DefaultSpendingCurrencyCode: options.DefaultSpendingCurrencyCode,
		DefaultDisplayCurrencyCode:  options.DefaultDisplayCurrencyCode,
	}
}

func toEffectiveOptionsJSON(options households.EffectiveOptions) effectiveOptionsJSON {
	return effectiveOptionsJSON{
		DefaultIncomeCurrencyCode:   options.DefaultIncomeCurrencyCode,
		DefaultSpendingCurrencyCode: options.DefaultSpendingCurrencyCode,
		DefaultDisplayCurrencyCode:  options.DefaultDisplayCurrencyCode,
	}
}

func toInvitationJSON(invitation households.Invitation) invitationJSON {
	var acceptedAt *string
	if invitation.AcceptedAt != nil {
		value := invitation.AcceptedAt.UTC().Format(time.RFC3339Nano)
		acceptedAt = &value
	}
	return invitationJSON{
		ID:              invitation.ID.String(),
		HouseholdID:     invitation.HouseholdID.String(),
		HouseholdName:   invitation.HouseholdName,
		Email:           invitation.Email,
		Role:            string(invitation.Role),
		InvitedByUserID: invitation.InvitedByUserID.String(),
		ExpiresAt:       invitation.ExpiresAt.UTC().Format(time.RFC3339Nano),
		AcceptedAt:      acceptedAt,
		CreatedAt:       invitation.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}
