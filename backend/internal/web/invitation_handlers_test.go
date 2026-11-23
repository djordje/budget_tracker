package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"budgettracker/internal/households"
)

type acceptingInvitationHouseholds struct {
	stubHouseholds
	invitation households.Invitation
}

func (s acceptingInvitationHouseholds) AcceptInvitation(context.Context, string, uuid.UUID) (households.Invitation, error) {
	return s.invitation, nil
}

func TestAcceptInvitationRedirectsToJoinedHousehold(t *testing.T) {
	h := newTestHandler(t)
	joined, preferred := uuid.New(), uuid.New()
	h.households = acceptingInvitationHouseholds{invitation: households.Invitation{HouseholdID: joined}}
	h.sessions = nil // Acceptance does not depend on changing the landing preference.
	request := householdRequest(h, http.MethodPost, "/invitations/token", &preferred)
	request.SetPathValue("token", "token")
	recorder := httptest.NewRecorder()
	h.AcceptInvitation(recorder, request)

	require.Equal(t, http.StatusSeeOther, recorder.Code)
	assert.Equal(t, householdURL(joined, "/entries?notice=invitation_accepted"), recorder.Header().Get("Location"))
}
