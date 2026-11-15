package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"budgettracker/internal/households"
)

func TestMyOptionsUsesCombinedView(t *testing.T) {
	householdID := uuid.New()
	income := "EUR"
	view := households.OptionsView{
		User: households.UserOptions{DefaultIncomeCurrencyCode: &income},
		Effective: households.EffectiveOptions{
			DefaultIncomeCurrencyCode:   "EUR",
			DefaultSpendingCurrencyCode: "RSD",
			DefaultDisplayCurrencyCode:  "RSD",
		},
	}
	calls := 0
	handler := NewHouseholdHandler(stubHouseholdService{
		getOptionsView: func(_ context.Context, _ uuid.UUID, gotHousehold uuid.UUID) (households.OptionsView, error) {
			calls++
			assert.Equal(t, householdID, gotHousehold, "household")
			return view, nil
		},
	}, discardLogger())

	request := httptest.NewRequest(http.MethodGet, "/api/v1/households/"+householdID.String()+"/my-options", nil)
	request.SetPathValue("householdID", householdID.String())
	recorder := serveEntryRequest(t, activeEntrySession(), handler.MyOptions, request)

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	assert.Equal(t, 1, calls, "options reads")

	var body myOptionsResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body), "decode response")
	require.NotNil(t, body.Options.DefaultIncomeCurrencyCode, "options = %+v", body.Options)
	assert.Equal(t, "EUR", *body.Options.DefaultIncomeCurrencyCode, "options = %+v", body.Options)
	assert.Equal(t, "RSD", body.Effective.DefaultSpendingCurrencyCode, "effective = %+v", body.Effective)
}

func TestUpdateMyOptionsReturnsCommittedView(t *testing.T) {
	householdID := uuid.New()
	display := "USD"
	view := households.OptionsView{
		User: households.UserOptions{DefaultDisplayCurrencyCode: &display},
		Effective: households.EffectiveOptions{
			DefaultIncomeCurrencyCode:   "RSD",
			DefaultSpendingCurrencyCode: "RSD",
			DefaultDisplayCurrencyCode:  "USD",
		},
	}
	viewCalls := 0
	handler := NewHouseholdHandler(stubHouseholdService{
		setUserOptions: func(context.Context, uuid.UUID, uuid.UUID, households.UpdateUserOptionsParams) (households.OptionsView, error) {
			return view, nil
		},
		getOptionsView: func(context.Context, uuid.UUID, uuid.UUID) (households.OptionsView, error) {
			viewCalls++
			return households.OptionsView{}, nil
		},
	}, discardLogger())

	request := httptest.NewRequest(http.MethodPut, "/api/v1/households/"+householdID.String()+"/my-options",
		strings.NewReader(`{"default_display_currency_code":"USD"}`))
	request.SetPathValue("householdID", householdID.String())
	recorder := serveEntryRequest(t, activeEntrySession(), handler.UpdateMyOptions, request)

	require.Equal(t, http.StatusOK, recorder.Code, "body = %s", recorder.Body.String())
	assert.Zero(t, viewCalls, "extra options reads")

	var response myOptionsResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response), "decode response")
	assert.Equal(t, "USD", response.Effective.DefaultDisplayCurrencyCode, "effective = %+v", response.Effective)
}

func TestRevokeInvitationBindsBothPathIDs(t *testing.T) {
	householdID := uuid.New()
	invitationID := uuid.New()

	t.Run("malformed household", func(t *testing.T) {
		called := false
		handler := NewHouseholdHandler(stubHouseholdService{
			revokeInvitation: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
				called = true
				return nil
			},
		}, discardLogger())
		request := httptest.NewRequest(http.MethodDelete, "/api/v1/households/nope/invitations/"+invitationID.String(), nil)
		request.SetPathValue("householdID", "nope")
		request.SetPathValue("invitationID", invitationID.String())
		recorder := serveEntryRequest(t, activeEntrySession(), handler.RevokeInvitation, request)

		assert.Equal(t, http.StatusNotFound, recorder.Code)
		assert.False(t, called, "service was called with a malformed household ID")
	})

	t.Run("mismatched pair", func(t *testing.T) {
		handler := NewHouseholdHandler(stubHouseholdService{
			revokeInvitation: func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
				return households.ErrInvitationNotFound
			},
		}, discardLogger())
		request := httptest.NewRequest(http.MethodDelete, "/api/v1/households/"+householdID.String()+"/invitations/"+invitationID.String(), nil)
		request.SetPathValue("householdID", householdID.String())
		request.SetPathValue("invitationID", invitationID.String())
		recorder := serveEntryRequest(t, activeEntrySession(), handler.RevokeInvitation, request)

		assert.Equal(t, http.StatusNotFound, recorder.Code)
	})

	t.Run("success passes both IDs", func(t *testing.T) {
		var gotHousehold, gotInvitation uuid.UUID
		handler := NewHouseholdHandler(stubHouseholdService{
			revokeInvitation: func(_ context.Context, _ uuid.UUID, householdID, invitationID uuid.UUID) error {
				gotHousehold, gotInvitation = householdID, invitationID
				return nil
			},
		}, discardLogger())
		request := httptest.NewRequest(http.MethodDelete, "/api/v1/households/"+householdID.String()+"/invitations/"+invitationID.String(), nil)
		request.SetPathValue("householdID", householdID.String())
		request.SetPathValue("invitationID", invitationID.String())
		recorder := serveEntryRequest(t, activeEntrySession(), handler.RevokeInvitation, request)

		assert.Equal(t, http.StatusNoContent, recorder.Code)
		assert.Equal(t, householdID, gotHousehold, "RevokeInvitation() household")
		assert.Equal(t, invitationID, gotInvitation, "RevokeInvitation() invitation")
	})
}
