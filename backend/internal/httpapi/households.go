package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
)

// HouseholdService is the household behavior needed by JSON handlers.
type HouseholdService interface {
	CreateAndActivate(ctx context.Context, userID uuid.UUID, sessionTokenHash, name string) (households.Household, error)
	List(ctx context.Context, userID uuid.UUID) ([]households.Household, error)
	Get(ctx context.Context, userID, householdID uuid.UUID) (households.Household, error)
	Members(ctx context.Context, userID, householdID uuid.UUID) ([]households.Member, error)
	GetOptions(ctx context.Context, userID, householdID uuid.UUID) (households.Options, error)
	GetOptionsView(ctx context.Context, userID, householdID uuid.UUID) (households.OptionsView, error)
	UpdateOptions(ctx context.Context, actorID, householdID uuid.UUID, params households.UpdateOptionsParams) (households.Options, error)
	SetUserOptions(ctx context.Context, userID, householdID uuid.UUID, params households.UpdateUserOptionsParams) (households.OptionsView, error)
	CreateInvitation(ctx context.Context, params households.CreateInvitationParams) (households.Invitation, string, error)
	ListInvitations(ctx context.Context, actorID, householdID uuid.UUID) ([]households.Invitation, error)
	RevokeInvitation(ctx context.Context, actorID, householdID, invitationID uuid.UUID) error
}

// HouseholdHandler serves the household JSON endpoints.
type HouseholdHandler struct {
	households HouseholdService
	logger     *slog.Logger
}

// NewHouseholdHandler constructs household JSON handlers.
func NewHouseholdHandler(households HouseholdService, logger *slog.Logger) *HouseholdHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &HouseholdHandler{households: households, logger: logger}
}

// RequireAuthAPI rejects unauthenticated API requests with a JSON 401.
func RequireAuthAPI(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.FromContext(r.Context()); !ok {
			WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
			return
		}
		next(w, r)
	}
}

// List returns the authenticated user's households.
func (h *HouseholdHandler) List(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}

	result, err := h.households.List(r.Context(), session.User.ID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	items := make([]householdJSON, 0, len(result))
	for _, household := range result {
		items = append(items, toHouseholdJSON(household))
	}
	WriteJSON(w, http.StatusOK, householdsResponse{Households: items})
}

// Create creates a household and makes it active in one committed workflow.
func (h *HouseholdHandler) Create(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}

	var request createHouseholdRequest
	if err := decodeJSON(w, r, &request); err != nil {
		WriteDecodeError(w, err)
		return
	}

	household, err := h.households.CreateAndActivate(
		r.Context(), session.User.ID, session.TokenHash, request.Name,
	)
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	w.Header().Set("Location", "/api/v1/households/"+household.ID.String())
	WriteJSON(w, http.StatusCreated, householdResponse{Household: toHouseholdJSON(household)})
}

// Get returns one household when the user is a member.
func (h *HouseholdHandler) Get(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := householdIDFromPath(w, r)
	if !ok {
		return
	}

	household, err := h.households.Get(r.Context(), session.User.ID, householdID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, householdResponse{Household: toHouseholdJSON(household)})
}

// Members returns the member list for admins.
func (h *HouseholdHandler) Members(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := householdIDFromPath(w, r)
	if !ok {
		return
	}

	members, err := h.households.Members(r.Context(), session.User.ID, householdID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	items := make([]memberJSON, 0, len(members))
	for _, member := range members {
		items = append(items, toMemberJSON(member))
	}
	WriteJSON(w, http.StatusOK, membersResponse{Members: items})
}

// Options returns the household defaults to members.
func (h *HouseholdHandler) Options(w http.ResponseWriter, r *http.Request) {
	session, ok := auth.FromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to access this resource.")
		return
	}
	householdID, ok := householdIDFromPath(w, r)
	if !ok {
		return
	}

	options, err := h.households.GetOptions(r.Context(), session.User.ID, householdID)
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

func (h *HouseholdHandler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *households.ValidationError
	var optionsValidation *households.OptionsValidationError
	switch {
	case errors.As(err, &validation):
		WriteValidationError(w, map[string]string{validation.Field: validation.Message})
	case errors.As(err, &optionsValidation):
		WriteValidationError(w, optionsValidation.Fields)
	case errors.Is(err, households.ErrNotFound), errors.Is(err, households.ErrInvitationNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "Household or invitation not found.")
	case errors.Is(err, auth.ErrSessionNotFound):
		WriteError(w, http.StatusUnauthorized, "unauthenticated", "Sign in again to continue.")
	case errors.Is(err, households.ErrForbidden):
		WriteError(w, http.StatusForbidden, "forbidden", "You do not have permission to do that.")
	case errors.Is(err, households.ErrInvitationExists):
		WriteError(w, http.StatusConflict, "invitation_exists", "A pending invitation already exists for that email.")
	case errors.Is(err, households.ErrAlreadyMember):
		WriteError(w, http.StatusConflict, "already_member", "That email already belongs to the household.")
	case errors.Is(err, households.ErrInvitationAccepted), errors.Is(err, households.ErrInvitationExpired):
		WriteError(w, http.StatusConflict, "invitation_unavailable", "That invitation is no longer available.")
	default:
		h.logger.Error("household request failed", "path", r.URL.Path, "error", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "The request could not be processed.")
	}
}

func householdIDFromPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("householdID"))
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", "Household not found.")
		return uuid.Nil, false
	}
	return id, true
}

type createHouseholdRequest struct {
	Name string `json:"name"`
}

type householdJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type householdResponse struct {
	Household householdJSON `json:"household"`
}

type householdsResponse struct {
	Households []householdJSON `json:"households"`
}

type memberJSON struct {
	UserID    string `json:"user_id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Role      string `json:"role"`
}

type membersResponse struct {
	Members []memberJSON `json:"members"`
}

type optionsJSON struct {
	DefaultIncomeCurrencyCode   string `json:"default_income_currency_code"`
	DefaultSpendingCurrencyCode string `json:"default_spending_currency_code"`
	DefaultDisplayCurrencyCode  string `json:"default_display_currency_code"`
}

type optionsResponse struct {
	Options optionsJSON `json:"options"`
}

func toHouseholdJSON(household households.Household) householdJSON {
	return householdJSON{
		ID:        household.ID.String(),
		Name:      household.Name,
		Role:      string(household.Role),
		CreatedAt: household.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: household.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func toMemberJSON(member households.Member) memberJSON {
	return memberJSON{
		UserID:    member.UserID.String(),
		FirstName: member.FirstName,
		LastName:  member.LastName,
		Email:     member.Email,
		Role:      string(member.Role),
	}
}
