package web

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
)

type invitationPageData struct {
	pageData
	Token        string
	Invitation   households.Invitation
	EmailMatches bool
	Expired      bool
	Accepted     bool
	LoginURL     string
	RegisterURL  string
	ActionError  string
}

// InvitationPage shows a public invitation and an accept action for the
// matching signed-in account.
func (h *Handler) InvitationPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		h.renderError(w, r, http.StatusNotFound, "Invitation not found", "That invitation link is not valid.")
		return
	}
	invitation, err := h.households.InvitationByToken(r.Context(), token)
	if errors.Is(err, households.ErrInvitationNotFound) {
		h.renderError(w, r, http.StatusNotFound, "Invitation not found", "That invitation link is not valid or has expired.")
		return
	}
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	h.render(w, r, "invitation", http.StatusOK, h.invitationData(r, token, invitation, ""))
}

// AcceptInvitation accepts the invitation for the signed-in matching account.
// Unlike ordinary protected routes, this handler owns the anonymous redirect so
// the visitor returns to the invitation with a safe next path after signing in.
func (h *Handler) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	session, ok := auth.FromContext(r.Context())
	if !ok {
		next := safeNextPath("/invitations/"+token, "/app")
		http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
		return
	}

	invitation, err := h.households.AcceptInvitation(r.Context(), token, session.User.ID)
	if err != nil {
		if errors.Is(err, households.ErrInvitationNotFound) {
			h.renderError(w, r, http.StatusNotFound, "Invitation not found", "That invitation link is not valid or has expired.")
			return
		}
		if errors.Is(err, households.ErrInvitationExpired) ||
			errors.Is(err, households.ErrInvitationAccepted) ||
			errors.Is(err, households.ErrInvitationEmailMismatch) ||
			errors.Is(err, households.ErrAlreadyMember) {
			current, loadErr := h.households.InvitationByToken(r.Context(), token)
			if loadErr != nil {
				h.renderError(w, r, http.StatusNotFound, "Invitation not found", "That invitation is no longer available.")
				return
			}
			message := "That invitation is no longer available."
			status := http.StatusConflict
			switch {
			case errors.Is(err, households.ErrInvitationEmailMismatch):
				message = "Sign in with the email address that was invited."
				status = http.StatusForbidden
			case errors.Is(err, households.ErrAlreadyMember):
				message = "This account already belongs to the household."
			}
			h.render(w, r, "invitation", status, h.invitationData(r, token, current, message))
			return
		}
		h.internalError(w, r, err)
		return
	}

	if err := h.sessions.SetActiveHousehold(r.Context(), session.TokenHash, session.User.ID, &invitation.HouseholdID); err != nil {
		// The membership committed with the acceptance, so the join stands. Tell
		// the visitor to select the household instead of failing the request.
		h.logger.Error("set active household after invite acceptance", "error", err)
		http.Redirect(w, r, "/app?notice=invitation_accepted_household_not_selected", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/app?notice=invitation_accepted", http.StatusSeeOther)
}

func (h *Handler) invitationData(r *http.Request, token string, invitation households.Invitation, actionError string) invitationPageData {
	data := invitationPageData{
		pageData:    h.basePageData(r, "Join "+invitation.HouseholdName),
		Token:       token,
		Invitation:  invitation,
		Expired:     invitation.Expired(time.Now().UTC()),
		Accepted:    invitation.AcceptedAt != nil,
		ActionError: actionError,
	}
	nextPath := "/invitations/" + token
	data.LoginURL = "/login?next=" + url.QueryEscape(nextPath)
	data.RegisterURL = "/register?next=" + url.QueryEscape(nextPath)
	if session, ok := auth.FromContext(r.Context()); ok {
		data.EmailMatches = session.User.Email == invitation.Email
	}
	return data
}
