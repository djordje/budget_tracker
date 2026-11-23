package web

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"budgettracker/internal/households"
)

func householdURL(id uuid.UUID, suffix string) string {
	return "/app/" + id.String() + suffix
}

type householdContextKey struct{}

type householdContext struct {
	Household   *households.Household
	Memberships []households.Household
	Section     string
	Page        string
}

func scopeFromRequest(r *http.Request) householdContext {
	scope, _ := r.Context().Value(householdContextKey{}).(householdContext)
	return scope
}

// Scoped authorizes the URL household before any page or mutation runs.
// Section and page come from the matched route registration.
func (h *Handler) Scoped(section, page string, next http.HandlerFunc) http.HandlerFunc {
	return RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		id, err := householdIDFromPath(r)
		if err != nil {
			h.renderSettingsError(w, r, households.ErrNotFound)
			return
		}
		list, err := h.households.List(r.Context(), sessionFromContext(r).User.ID)
		if err != nil {
			h.internalError(w, r, err)
			return
		}
		selected := findHousehold(list, id)
		if selected == nil {
			h.renderSettingsError(w, r, households.ErrNotFound)
			return
		}
		scope := householdContext{Household: selected, Memberships: list, Section: section, Page: page}
		next(w, r.WithContext(context.WithValue(r.Context(), householdContextKey{}, scope)))
	})
}

// App redirects via the last-used accessible household, or the first
// membership in the service's stable ordering if that preference is stale.
func (h *Handler) App(w http.ResponseWriter, r *http.Request) {
	session := sessionFromContext(r)
	list, err := h.households.List(r.Context(), session.User.ID)
	if err != nil {
		h.internalError(w, r, err)
		return
	}
	if len(list) == 0 {
		http.Redirect(w, r, "/app/households/new", http.StatusSeeOther)
		return
	}
	selected := &list[0]
	if session.ActiveHouseholdID != nil {
		if preferred := findHousehold(list, *session.ActiveHouseholdID); preferred != nil {
			selected = preferred
		}
	}
	http.Redirect(w, r, householdURL(selected.ID, ""), http.StatusSeeOther)
}

// HouseholdHome redirects to the canonical Entries page.
func (h *Handler) HouseholdHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, householdURL(scopeFromRequest(r).Household.ID, "/entries"), http.StatusSeeOther)
}

func (h *Handler) rememberHousehold(r *http.Request) {
	selected := scopeFromRequest(r).Household
	if selected == nil || r.Method != http.MethodGet || isHTMX(r) {
		return
	}
	session := sessionFromContext(r)
	if session.ActiveHouseholdID != nil && *session.ActiveHouseholdID == selected.ID {
		return
	}
	if err := h.sessions.SetActiveHousehold(r.Context(), session.TokenHash, session.User.ID, &selected.ID); err != nil {
		h.logger.Warn("remember household", "error", err)
	}
}
