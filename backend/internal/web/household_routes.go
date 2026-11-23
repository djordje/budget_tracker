package web

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"budgettracker/internal/households"
)

// errMissingScope reports a handler that ran without Scoped. It is a wiring
// bug, not something a request can trigger.
var errMissingScope = errors.New("missing household scope")

func householdURL(id uuid.UUID, suffix string) string {
	return "/app/" + id.String() + suffix
}

// Section is the top-level destination the shared navigation marks as active.
type Section string

// Page is the exact destination the shared navigation marks with aria-current.
type Page string

// Sections and pages are matched when the route is registered, never from the
// request URL. templates/_navigation.html compares against these values.
const (
	SectionEntries Section = "entries"
	SectionReports Section = "reports"
	SectionMore    Section = "more"

	PageEntries       Page = "entries"
	PageReports       Page = "reports"
	PageExchangeRates Page = "exchange-rates"
	PagePreferences   Page = "preferences"
	PageSettings      Page = "settings"
)

type householdContextKey struct{}

type householdContext struct {
	Household   *households.Household
	Memberships []households.Household
	Section     Section
	Page        Page
}

func scopeFromRequest(r *http.Request) householdContext {
	scope, _ := r.Context().Value(householdContextKey{}).(householdContext)
	return scope
}

// scopedHousehold returns the household resolved by Scoped. Handlers behind
// Scoped can read it from pageData, but private helpers that build page data
// before rendering use this to fail cleanly instead of dereferencing nil.
func scopedHousehold(r *http.Request) (*households.Household, error) {
	if selected := scopeFromRequest(r).Household; selected != nil {
		return selected, nil
	}
	return nil, errMissingScope
}

// Scoped authenticates the request, then authorizes the URL household before
// any page or mutation runs. Section and page come from the matched route
// registration.
func (h *Handler) Scoped(section Section, page Page, next http.HandlerFunc) http.HandlerFunc {
	return RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		id, err := householdIDFromPath(r)
		if err != nil {
			// A single-segment path such as a retired /app/entries bookmark lands
			// here, so report it as a missing page rather than a missing household.
			h.renderError(w, r, http.StatusNotFound, "Page not found", "That page is not available.")
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
	if selected == nil || r.Method != http.MethodGet || isHTMX(r) || isPrefetch(r) {
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

// isPrefetch reports speculative loads. Remembering the household on those
// would move the landing preference when a browser merely preloads a picker
// link the visitor never follows.
func isPrefetch(r *http.Request) bool {
	for _, header := range []string{"Sec-Purpose", "Purpose", "X-Purpose"} {
		if strings.Contains(strings.ToLower(r.Header.Get(header)), "prefetch") {
			return true
		}
	}
	return false
}
