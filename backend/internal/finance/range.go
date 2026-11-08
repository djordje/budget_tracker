package finance

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"budgettracker/internal/households"
)

// EntriesForRange returns every entry in a date range for reporting. It uses
// the same scope and household authorization rules as List.
func (s *Service) EntriesForRange(
	ctx context.Context,
	householdID, actorID uuid.UUID,
	scope Scope,
	from, to time.Time,
) ([]Entry, error) {
	return s.EntriesForRangeTx(ctx, s.pool, householdID, actorID, scope, from, to)
}

// EntriesForRangeTx reads entries through the provided query source so a
// reporting transaction can keep its snapshot consistent.
func (s *Service) EntriesForRangeTx(
	ctx context.Context,
	db Querier,
	householdID, actorID uuid.UUID,
	scope Scope,
	from, to time.Time,
) ([]Entry, error) {
	if scope == "" {
		scope = ScopeMine
	}
	if scope != ScopeMine && scope != ScopeAll {
		return nil, validationError("scope", "Choose mine or all.")
	}
	if from.IsZero() || to.IsZero() {
		return nil, validationError("from", "Choose a valid date range.")
	}
	if dateOnly(from).After(dateOnly(to)) {
		return nil, validationError("from", "The start date must not be after the end date.")
	}

	role, err := s.membershipTx(ctx, db, householdID, actorID)
	if err != nil {
		return nil, err
	}
	if scope == ScopeAll && role != households.RoleAdmin {
		return nil, ErrForbidden
	}

	entries := make([]Entry, 0)
	for _, kind := range []Kind{KindIncome, KindSpending} {
		kindEntries, err := s.entriesForRangeKind(ctx, db, kind, householdID, actorID, scope, from, to)
		if err != nil {
			return nil, err
		}
		entries = append(entries, kindEntries...)
	}
	return entries, nil
}

func (s *Service) entriesForRangeKind(
	ctx context.Context,
	db Querier,
	kind Kind,
	householdID, actorID uuid.UUID,
	scope Scope,
	from, to time.Time,
) ([]Entry, error) {
	conditions := []string{"e.household_id = $1", "e.entry_date >= $2::date", "e.entry_date <= $3::date"}
	args := []any{householdID, dateOnly(from).Format(entryDateLayout), dateOnly(to).Format(entryDateLayout)}
	if scope == ScopeMine {
		conditions = append(conditions, fmt.Sprintf("e.user_id = $%d", len(args)+1))
		args = append(args, actorID)
	}

	rows, err := db.Query(ctx, fmt.Sprintf(`
		SELECT e.id, e.household_id, e.user_id, e.amount_minor, e.currency_code,
		       c.minor_units, e.entry_date, e.description, e.created_at, e.updated_at,
		       COALESCE(u.first_name, ''), COALESCE(u.last_name, '')
		FROM %s e
		JOIN currencies c ON c.code = e.currency_code
		LEFT JOIN users u ON u.id = e.user_id
		WHERE %s
		ORDER BY e.entry_date, e.created_at, e.id`,
		kind.table(), strings.Join(conditions, " AND ")),
		args...)
	if err != nil {
		return nil, fmt.Errorf("list %s for report: %w", kind, err)
	}
	defer rows.Close()

	entries := make([]Entry, 0)
	for rows.Next() {
		entry, err := scanEntry(rows, kind)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s for report: %w", kind, err)
	}
	return entries, nil
}
