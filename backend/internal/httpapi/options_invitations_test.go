package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
			if gotHousehold != householdID {
				t.Errorf("household = %s, want %s", gotHousehold, householdID)
			}
			return view, nil
		},
	}, discardLogger())

	request := httptest.NewRequest(http.MethodGet, "/api/v1/households/"+householdID.String()+"/my-options", nil)
	request.SetPathValue("householdID", householdID.String())
	recorder := serveEntryRequest(t, activeEntrySession(), handler.MyOptions, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if calls != 1 {
		t.Errorf("options reads = %d, want 1", calls)
	}
	var body myOptionsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Options.DefaultIncomeCurrencyCode == nil || *body.Options.DefaultIncomeCurrencyCode != "EUR" {
		t.Errorf("options = %+v", body.Options)
	}
	if body.Effective.DefaultSpendingCurrencyCode != "RSD" {
		t.Errorf("effective = %+v", body.Effective)
	}
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

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if viewCalls != 0 {
		t.Errorf("handler performed %d extra options reads, want 0", viewCalls)
	}
	var response myOptionsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Effective.DefaultDisplayCurrencyCode != "USD" {
		t.Errorf("effective = %+v", response.Effective)
	}
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

		if recorder.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
		if called {
			t.Error("service was called with a malformed household ID")
		}
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

		if recorder.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
		}
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

		if recorder.Code != http.StatusNoContent {
			t.Errorf("status = %d, want %d", recorder.Code, http.StatusNoContent)
		}
		if gotHousehold != householdID || gotInvitation != invitationID {
			t.Errorf("RevokeInvitation(%s, %s)", gotHousehold, gotInvitation)
		}
	})
}
