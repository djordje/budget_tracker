package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
	"budgettracker/internal/households"
)

// acceptingInvitationHouseholds accepts every invitation so the handler reaches
// the active-household step.
type acceptingInvitationHouseholds struct {
	stubHouseholds
	invitation households.Invitation
}

func (s acceptingInvitationHouseholds) AcceptInvitation(context.Context, string, uuid.UUID) (households.Invitation, error) {
	return s.invitation, nil
}

func TestAcceptInvitationSurfacesActiveHouseholdFailure(t *testing.T) {
	householdID := uuid.New()
	tests := []struct {
		name               string
		setActiveHousehold error
		wantLocation       string
		wantLog            bool
	}{
		{
			name:         "active household set",
			wantLocation: "/app?notice=invitation_accepted",
		},
		{
			name:               "active household failed",
			setActiveHousehold: errors.New("session store unavailable"),
			wantLocation:       "/app?notice=invitation_accepted_household_not_selected",
			wantLog:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			handler, err := New(Deps{
				Logger:     bufferedLogger(&logs),
				Sessions:   stubSessions{err: tt.setActiveHousehold},
				Households: acceptingInvitationHouseholds{invitation: households.Invitation{HouseholdID: householdID}},
				Entries:    stubEntries{},
				Rates:      stubRates{},
				Reports:    stubReports{},
			})
			require.NoError(t, err, "New()")

			userID := uuid.New()
			session := auth.Session{
				UserID:    userID,
				User:      auth.User{ID: userID},
				TokenHash: strings.Repeat("a", 64),
			}
			request := httptest.NewRequest(http.MethodPost, "/invitations/token/accept", nil)
			request.SetPathValue("token", "token")
			request.AddCookie(&http.Cookie{Name: auth.DefaultSessionCookieName, Value: "raw"})
			recorder := httptest.NewRecorder()
			auth.NewMiddleware(stubSessionLookup{session: session}, auth.CookieConfig{}, bufferedLogger(&logs)).
				Load(http.HandlerFunc(handler.AcceptInvitation)).ServeHTTP(recorder, request)

			require.Equal(t, http.StatusSeeOther, recorder.Code, "body = %s", recorder.Body.String())
			assert.Equal(t, tt.wantLocation, recorder.Header().Get("Location"), "Location")
			assert.NotContains(t, recorder.Body.String(), "Something went wrong", "failed activation rendered an internal error")
			if tt.wantLog {
				assert.Contains(t, logs.String(), "level=ERROR", "failed activation was not logged at error level")
				assert.Contains(t, logs.String(), "set active household after invite acceptance", "failed activation log")
			}
		})
	}
}
