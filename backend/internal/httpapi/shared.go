package httpapi

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"budgettracker/internal/auth"
)

func activeHouseholdID(w http.ResponseWriter, session auth.Session) (uuid.UUID, bool) {
	if session.ActiveHouseholdID == nil {
		WriteValidationError(w, map[string]string{
			"household_id": "Select a household before managing entries.",
		})
		return uuid.Nil, false
	}
	return *session.ActiveHouseholdID, true
}

func entryIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("entryID"), 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, http.StatusNotFound, "not_found", "Entry not found.")
		return 0, false
	}
	return id, true
}
