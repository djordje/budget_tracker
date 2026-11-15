package finance

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"budgettracker/internal/households"
)

var (
	// ErrNotFound means the entry or household is not visible to the actor.
	ErrNotFound = households.ErrNotFound
	// ErrForbidden means the actor can see the entry but cannot modify it.
	ErrForbidden = households.ErrForbidden

	errUnknownCurrency = errors.New("unknown currency")
)

const (
	entryDateLayout = "2006-01-02"
	defaultPageSize = 20
	maxPageSize     = 100
)

// Kind identifies the entry table.
type Kind string

const (
	KindIncome   Kind = "income"
	KindSpending Kind = "spending"
)

func (k Kind) valid() bool {
	return k == KindIncome || k == KindSpending
}

func (k Kind) table() string {
	if k == KindIncome {
		return "incomes"
	}
	return "spendings"
}

// Scope controls personal versus household-wide visibility.
type Scope string

const (
	ScopeMine Scope = "mine"
	ScopeAll  Scope = "all"
)

// ParseScope parses a user-supplied scope, defaulting to mine.
func ParseScope(raw string) (Scope, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(ScopeMine):
		return ScopeMine, nil
	case string(ScopeAll):
		return ScopeAll, nil
	default:
		return "", fmt.Errorf("invalid scope %q", raw)
	}
}

// ValidationError describes invalid entry input by field.
type ValidationError struct {
	Fields map[string]string
}

// Error implements error.
func (e *ValidationError) Error() string {
	return "validation failed"
}

func newValidationError() *ValidationError {
	return &ValidationError{Fields: make(map[string]string)}
}

func (e *ValidationError) add(field, message string) {
	if _, exists := e.Fields[field]; !exists {
		e.Fields[field] = message
	}
}

// validationMessage is user-facing copy for one validation field. It is
// deliberately not an error: Go error text is a lowercase diagnostic, while
// this sentence is rendered verbatim and keeps its capital letter and final
// period.
type validationMessage struct {
	text string
}

// Entry is an income or spending row. CurrencyMinorUnits is loaded from the
// currency table for formatting and is not part of the entry row.
type Entry struct {
	ID                 int64
	Kind               Kind
	HouseholdID        uuid.UUID
	UserID             *uuid.UUID
	OwnerFirstName     string
	OwnerLastName      string
	AmountMinor        int64
	CurrencyCode       string
	CurrencyMinorUnits int32
	EntryDate          time.Time
	Description        *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// IsHouseholdWide reports whether the entry is attributed to the household.
func (e Entry) IsHouseholdWide() bool {
	return e.UserID == nil
}

// AmountString formats the amount without grouping for forms and APIs.
func (e Entry) AmountString() string {
	return FormatAmount(e.AmountMinor, e.CurrencyMinorUnits)
}

// DisplayAmount formats the amount with grouped thousands for HTML.
func (e Entry) DisplayAmount() string {
	return DisplayAmount(e.AmountMinor, e.CurrencyMinorUnits)
}

// DateString formats the entry date for HTML form values and APIs.
func (e Entry) DateString() string {
	return e.EntryDate.Format(entryDateLayout)
}

// CreateParams contains the input for a new entry. Empty date and currency
// values receive household/current-date defaults in the service.
type CreateParams struct {
	Kind          Kind
	HouseholdID   uuid.UUID
	ActorID       uuid.UUID
	Amount        string
	CurrencyCode  string
	EntryDate     time.Time
	Description   string
	HouseholdWide bool
}

// UpdateParams contains optional entry changes. Nil fields remain unchanged;
// a pointer to an empty description clears it.
type UpdateParams struct {
	Kind         Kind
	ID           int64
	ActorID      uuid.UUID
	Amount       *string
	CurrencyCode *string
	EntryDate    *time.Time
	Description  *string
}

// ListParams filters and paginates one entry kind.
type ListParams struct {
	Kind        Kind
	HouseholdID uuid.UUID
	ActorID     uuid.UUID
	Scope       Scope
	From        *time.Time
	To          *time.Time
	Page        int
	PageSize    int
}

// ListResult is a page of entries.
type ListResult struct {
	Entries    []Entry
	Page       int
	PageSize   int
	Total      int
	TotalPages int
}

// Querier is the shared read interface implemented by the pool and a
// transaction. Reporting uses it to keep every summary read in one snapshot.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Service owns entry persistence, defaults, and authorization.
type Service struct {
	pool *pgxpool.Pool
}

// NewService constructs an entry service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Create validates and stores an entry.
func (s *Service) Create(ctx context.Context, params CreateParams) (Entry, error) {
	if !params.Kind.valid() {
		return Entry{}, validationError("kind", "Unknown entry type.")
	}

	role, err := s.membership(ctx, params.HouseholdID, params.ActorID)
	if err != nil {
		return Entry{}, err
	}
	if params.HouseholdWide && role != households.RoleAdmin {
		return Entry{}, ErrForbidden
	}

	validation := newValidationError()
	currencyCode := strings.ToUpper(strings.TrimSpace(params.CurrencyCode))
	if currencyCode == "" {
		currencyCode, err = s.defaultCurrencyCode(ctx, params.HouseholdID, params.ActorID, params.Kind)
		if err != nil {
			return Entry{}, err
		}
	}

	currency, err := s.currencyByCode(ctx, currencyCode)
	if errors.Is(err, errUnknownCurrency) {
		validation.add("currency_code", "Choose a supported currency.")
	} else if err != nil {
		return Entry{}, err
	}

	var amountMinor int64
	if len(validation.Fields) == 0 {
		amountMinor, err = ParseAmount(params.Amount, currency.MinorUnits)
		if err != nil {
			validation.add("amount", amountMessage(currency.MinorUnits))
		}
	}

	entryDate := params.EntryDate
	if entryDate.IsZero() {
		entryDate = today()
	}
	entryDate = dateOnly(entryDate)

	description, descriptionMessage := normalizeDescription(params.Description)
	if descriptionMessage != nil {
		validation.add("description", descriptionMessage.text)
	}
	if len(validation.Fields) > 0 {
		return Entry{}, validation
	}

	var userID *uuid.UUID
	if !params.HouseholdWide {
		userID = &params.ActorID
	}

	var entry Entry
	entry.Kind = params.Kind
	entry.HouseholdID = params.HouseholdID
	entry.UserID = userID
	entry.AmountMinor = amountMinor
	entry.CurrencyCode = currency.Code
	entry.CurrencyMinorUnits = currency.MinorUnits
	entry.EntryDate = entryDate
	entry.Description = description

	err = s.pool.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO %s (household_id, user_id, amount_minor, currency_code, entry_date, description)
		VALUES ($1, $2, $3, $4, $5::date, $6)
		RETURNING id, created_at, updated_at`, params.Kind.table()),
		entry.HouseholdID, entry.UserID, entry.AmountMinor, entry.CurrencyCode,
		entry.EntryDate.Format(entryDateLayout), entry.Description,
	).Scan(&entry.ID, &entry.CreatedAt, &entry.UpdatedAt)
	if err != nil {
		return Entry{}, fmt.Errorf("insert entry: %w", err)
	}
	return entry, nil
}

// Get returns an entry visible to the actor.
func (s *Service) Get(ctx context.Context, actorID uuid.UUID, kind Kind, id int64) (Entry, error) {
	if !kind.valid() {
		return Entry{}, ErrNotFound
	}
	entry, err := s.loadEntry(ctx, kind, id)
	if err != nil {
		return Entry{}, err
	}

	role, err := s.membership(ctx, entry.HouseholdID, actorID)
	if err != nil {
		return Entry{}, err
	}
	policy := NewEntryPolicy(actorID, role == households.RoleAdmin)
	if !policy.CanView(entry) {
		return Entry{}, ErrNotFound
	}
	return entry, nil
}

// Update changes an entry that the actor is allowed to modify.
func (s *Service) Update(ctx context.Context, params UpdateParams) (Entry, error) {
	if !params.Kind.valid() {
		return Entry{}, ErrNotFound
	}
	current, err := s.loadEntry(ctx, params.Kind, params.ID)
	if err != nil {
		return Entry{}, err
	}
	role, err := s.membership(ctx, current.HouseholdID, params.ActorID)
	if err != nil {
		return Entry{}, err
	}
	policy := NewEntryPolicy(params.ActorID, role == households.RoleAdmin)
	if !policy.CanModify(current) {
		if !policy.CanView(current) {
			return Entry{}, ErrNotFound
		}
		return Entry{}, ErrForbidden
	}

	validation := newValidationError()
	amountMinor := current.AmountMinor
	currency := Currency{
		Code:       current.CurrencyCode,
		MinorUnits: current.CurrencyMinorUnits,
	}
	entryDate := current.EntryDate
	description := current.Description

	if params.CurrencyCode != nil {
		code := strings.ToUpper(strings.TrimSpace(*params.CurrencyCode))
		switch {
		case code == "":
			validation.add("currency_code", "Choose a currency.")
		case code != current.CurrencyCode:
			resolved, resolveErr := s.currencyByCode(ctx, code)
			if errors.Is(resolveErr, errUnknownCurrency) {
				validation.add("currency_code", "Choose a supported currency.")
			} else if resolveErr != nil {
				return Entry{}, resolveErr
			} else {
				currency = resolved
				if params.Amount == nil {
					validation.add("amount", "Enter an amount when changing currency.")
				}
			}
		}
	}

	if params.Amount != nil && len(validation.Fields) == 0 {
		amountMinor, err = ParseAmount(*params.Amount, currency.MinorUnits)
		if err != nil {
			validation.add("amount", amountMessage(currency.MinorUnits))
		}
	}
	if params.EntryDate != nil {
		if params.EntryDate.IsZero() {
			validation.add("entry_date", "Enter a valid date.")
		} else {
			entryDate = dateOnly(*params.EntryDate)
		}
	}
	if params.Description != nil {
		normalized, descriptionMessage := normalizeDescription(*params.Description)
		if descriptionMessage != nil {
			validation.add("description", descriptionMessage.text)
		} else {
			description = normalized
		}
	}
	if len(validation.Fields) > 0 {
		return Entry{}, validation
	}

	var updatedAt time.Time
	err = s.pool.QueryRow(ctx, fmt.Sprintf(`
		UPDATE %s
		SET amount_minor = $2, currency_code = $3, entry_date = $4::date,
		    description = $5, updated_at = now()
		WHERE id = $1
		RETURNING updated_at`, params.Kind.table()),
		current.ID, amountMinor, currency.Code, entryDate.Format(entryDateLayout), description,
	).Scan(&updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("update entry: %w", err)
	}

	current.AmountMinor = amountMinor
	current.CurrencyCode = currency.Code
	current.CurrencyMinorUnits = currency.MinorUnits
	current.EntryDate = entryDate
	current.Description = description
	current.UpdatedAt = updatedAt
	return current, nil
}

// Delete removes an entry that the actor is allowed to modify.
func (s *Service) Delete(ctx context.Context, actorID uuid.UUID, kind Kind, id int64) error {
	if !kind.valid() {
		return ErrNotFound
	}
	current, err := s.loadEntry(ctx, kind, id)
	if err != nil {
		return err
	}
	role, err := s.membership(ctx, current.HouseholdID, actorID)
	if err != nil {
		return err
	}
	policy := NewEntryPolicy(actorID, role == households.RoleAdmin)
	if !policy.CanModify(current) {
		if !policy.CanView(current) {
			return ErrNotFound
		}
		return ErrForbidden
	}

	if _, err := s.pool.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = $1", kind.table()), id); err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	return nil
}

// List returns a paginated page of entries for the active household.
func (s *Service) List(ctx context.Context, params ListParams) (ListResult, error) {
	if !params.Kind.valid() {
		return ListResult{}, validationError("kind", "Unknown entry type.")
	}
	if params.Scope == "" {
		params.Scope = ScopeMine
	}
	if params.Scope != ScopeMine && params.Scope != ScopeAll {
		return ListResult{}, validationError("scope", "Choose mine or all.")
	}
	if params.Page < 0 || params.PageSize < 0 {
		return ListResult{}, validationError("page", "Page values cannot be negative.")
	}
	if params.Page == 0 {
		params.Page = 1
	}
	if params.PageSize == 0 {
		params.PageSize = defaultPageSize
	}
	if params.PageSize > maxPageSize {
		return ListResult{}, validationError("page_size", fmt.Sprintf("Page size must be %d or fewer.", maxPageSize))
	}
	pageIndex := params.Page - 1
	if pageIndex > math.MaxInt/params.PageSize {
		return ListResult{}, validationError("page", "Page is too large.")
	}
	offset := pageIndex * params.PageSize
	if params.From != nil && params.To != nil && dateOnly(*params.From).After(dateOnly(*params.To)) {
		return ListResult{}, validationError("from", "The start date must not be after the end date.")
	}

	role, err := s.membership(ctx, params.HouseholdID, params.ActorID)
	if err != nil {
		return ListResult{}, err
	}
	if params.Scope == ScopeAll && role != households.RoleAdmin {
		return ListResult{}, ErrForbidden
	}

	table := params.Kind.table()
	conditions := []string{"e.household_id = $1"}
	args := []any{params.HouseholdID}
	if params.Scope == ScopeMine {
		conditions = append(conditions, fmt.Sprintf("e.user_id = $%d", len(args)+1))
		args = append(args, params.ActorID)
	}
	if params.From != nil {
		conditions = append(conditions, fmt.Sprintf("e.entry_date >= $%d::date", len(args)+1))
		args = append(args, dateOnly(*params.From).Format(entryDateLayout))
	}
	if params.To != nil {
		conditions = append(conditions, fmt.Sprintf("e.entry_date <= $%d::date", len(args)+1))
		args = append(args, dateOnly(*params.To).Format(entryDateLayout))
	}
	where := strings.Join(conditions, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(
		"SELECT count(*) FROM %s e WHERE %s", table, where,
	), args...).Scan(&total); err != nil {
		return ListResult{}, fmt.Errorf("count entries: %w", err)
	}

	listArgs := append(append([]any{}, args...), params.PageSize, offset)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT e.id, e.household_id, e.user_id, e.amount_minor, e.currency_code,
		       c.minor_units, e.entry_date, e.description, e.created_at, e.updated_at,
		       COALESCE(u.first_name, ''), COALESCE(u.last_name, '')
		FROM %s e
		JOIN currencies c ON c.code = e.currency_code
		LEFT JOIN users u ON u.id = e.user_id
		WHERE %s
		ORDER BY e.entry_date DESC, e.created_at DESC, e.id DESC
		LIMIT $%d OFFSET $%d`, table, where, len(args)+1, len(args)+2),
		listArgs...)
	if err != nil {
		return ListResult{}, fmt.Errorf("list entries: %w", err)
	}
	defer rows.Close()

	entries := make([]Entry, 0, params.PageSize)
	for rows.Next() {
		entry, err := scanEntry(rows, params.Kind)
		if err != nil {
			return ListResult{}, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, fmt.Errorf("iterate entries: %w", err)
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + params.PageSize - 1) / params.PageSize
	}
	return ListResult{
		Entries:    entries,
		Page:       params.Page,
		PageSize:   params.PageSize,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}

func (s *Service) membership(ctx context.Context, householdID, userID uuid.UUID) (households.Role, error) {
	return s.membershipTx(ctx, s.pool, householdID, userID)
}

func (s *Service) membershipTx(ctx context.Context, db Querier, householdID, userID uuid.UUID) (households.Role, error) {
	var role string
	err := db.QueryRow(ctx, `
		SELECT role
		FROM household_memberships
		WHERE household_id = $1 AND user_id = $2`,
		householdID, userID,
	).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("look up household membership: %w", err)
	}
	return households.Role(role), nil
}

func (s *Service) loadEntry(ctx context.Context, kind Kind, id int64) (Entry, error) {
	row := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT e.id, e.household_id, e.user_id, e.amount_minor, e.currency_code,
		       c.minor_units, e.entry_date, e.description, e.created_at, e.updated_at,
		       COALESCE(u.first_name, ''), COALESCE(u.last_name, '')
		FROM %s e
		JOIN currencies c ON c.code = e.currency_code
		LEFT JOIN users u ON u.id = e.user_id
		WHERE e.id = $1`, kind.table()), id)
	entry, err := scanEntry(row, kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func scanEntry(row pgx.Row, kind Kind) (Entry, error) {
	var entry Entry
	var minorUnits int16
	if err := row.Scan(
		&entry.ID, &entry.HouseholdID, &entry.UserID, &entry.AmountMinor, &entry.CurrencyCode,
		&minorUnits, &entry.EntryDate, &entry.Description, &entry.CreatedAt, &entry.UpdatedAt,
		&entry.OwnerFirstName, &entry.OwnerLastName,
	); err != nil {
		return Entry{}, err
	}
	entry.Kind = kind
	entry.CurrencyMinorUnits = int32(minorUnits)
	return entry, nil
}

// EntryPolicy is the single view/modify policy for an entry. Services use it
// for enforcement and HTML handlers use it for presentation decisions.
type EntryPolicy struct {
	actorID uuid.UUID
	isAdmin bool
}

// NewEntryPolicy constructs the policy for an actor in a household.
func NewEntryPolicy(actorID uuid.UUID, isAdmin bool) EntryPolicy {
	return EntryPolicy{actorID: actorID, isAdmin: isAdmin}
}

// CanView reports whether the actor may see the entry.
func (p EntryPolicy) CanView(entry Entry) bool {
	if p.isAdmin {
		return true
	}
	return entry.UserID != nil && *entry.UserID == p.actorID
}

// CanModify reports whether the actor may change or delete the entry.
func (p EntryPolicy) CanModify(entry Entry) bool {
	if entry.UserID == nil {
		return p.isAdmin
	}
	return *entry.UserID == p.actorID
}

var (
	descriptionCharactersMessage = &validationMessage{text: "Description contains invalid characters."}
	descriptionLengthMessage     = &validationMessage{text: "Description must be 500 characters or fewer."}
)

func normalizeDescription(raw string) (*string, *validationMessage) {
	description := strings.TrimSpace(raw)
	if description == "" {
		return nil, nil
	}
	if !utf8.ValidString(description) || strings.IndexByte(description, 0) >= 0 {
		return nil, descriptionCharactersMessage
	}
	if utf8.RuneCountInString(description) > 500 {
		return nil, descriptionLengthMessage
	}
	return &description, nil
}

func amountMessage(minorUnits int32) string {
	if minorUnits == 0 {
		return "Enter a positive amount without decimals."
	}
	return fmt.Sprintf("Enter a positive amount with at most %d decimal places.", minorUnits)
}

// Today returns the current UTC date at midnight. It is the single source for
// every user-visible default date.
func Today() time.Time {
	return today()
}

// TodayIn returns the UTC date of now at midnight.
func TodayIn(now time.Time) time.Time {
	return dateOnly(now.UTC())
}

func today() time.Time {
	return TodayIn(time.Now())
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

// ParseOptionalDate parses a user-supplied date in the wire format. An empty
// value after trimming means "no date". Both adapters use this parser so they
// accept exactly the same input.
func ParseOptionalDate(raw string) (*time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(entryDateLayout, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func validationError(field, message string) *ValidationError {
	validation := newValidationError()
	validation.add(field, message)
	return validation
}
