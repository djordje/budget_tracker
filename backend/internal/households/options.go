package households

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"budgettracker/internal/store"
)

// OptionsView contains household defaults, personal overrides, and resolved
// effective options from one consistent read.
type OptionsView struct {
	Household Options
	User      UserOptions
	Effective EffectiveOptions
}

// UserOptions contains nullable household-specific currency overrides.
type UserOptions struct {
	DefaultIncomeCurrencyCode   *string
	DefaultSpendingCurrencyCode *string
	DefaultDisplayCurrencyCode  *string
}

// EffectiveOptions contains resolved currency codes after inheritance.
type EffectiveOptions struct {
	DefaultIncomeCurrencyCode   string
	DefaultSpendingCurrencyCode string
	DefaultDisplayCurrencyCode  string
}

// UpdateOptionsParams contains household default currency changes.
type UpdateOptionsParams struct {
	DefaultIncomeCurrencyCode   string
	DefaultSpendingCurrencyCode string
	DefaultDisplayCurrencyCode  string
}

// UpdateUserOptionsParams contains personal override changes. Nil or empty
// values clear the override and inherit the household default.
type UpdateUserOptionsParams struct {
	DefaultIncomeCurrencyCode   *string
	DefaultSpendingCurrencyCode *string
	DefaultDisplayCurrencyCode  *string
}

// UpdateOptions changes household defaults. Only admins may update them.
func (s *Service) UpdateOptions(
	ctx context.Context,
	actorID, householdID uuid.UUID,
	params UpdateOptionsParams,
) (Options, error) {
	if _, err := s.adminRole(ctx, householdID, actorID); err != nil {
		return Options{}, err
	}

	validation := newOptionsValidation()
	income, err := s.validateCurrency(ctx, params.DefaultIncomeCurrencyCode, "default_income_currency_code", validation)
	if err != nil {
		return Options{}, err
	}
	spending, err := s.validateCurrency(ctx, params.DefaultSpendingCurrencyCode, "default_spending_currency_code", validation)
	if err != nil {
		return Options{}, err
	}
	display, err := s.validateCurrency(ctx, params.DefaultDisplayCurrencyCode, "default_display_currency_code", validation)
	if err != nil {
		return Options{}, err
	}
	if err := validation.error(); err != nil {
		return Options{}, err
	}

	row := s.pool.QueryRow(ctx, `
		UPDATE household_options
		SET default_income_currency_code = $2,
		    default_spending_currency_code = $3,
		    default_display_currency_code = $4,
		    updated_at = now()
		WHERE household_id = $1
		RETURNING default_income_currency_code, default_spending_currency_code,
		          default_display_currency_code`,
		householdID, income, spending, display,
	)
	var options Options
	err = row.Scan(
		&options.DefaultIncomeCurrencyCode,
		&options.DefaultSpendingCurrencyCode,
		&options.DefaultDisplayCurrencyCode,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Options{}, ErrNotFound
	}
	if err != nil {
		return Options{}, fmt.Errorf("update household options: %w", err)
	}
	return options, nil
}

// GetOptionsView returns household defaults, personal overrides, and resolved
// effective options from one query with membership authorization.
func (s *Service) GetOptionsView(ctx context.Context, userID, householdID uuid.UUID) (OptionsView, error) {
	return s.optionsView(ctx, s.pool, userID, householdID)
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Service) optionsView(
	ctx context.Context,
	db rowQuerier,
	userID, householdID uuid.UUID,
) (OptionsView, error) {
	var view OptionsView
	err := db.QueryRow(ctx, `
		SELECT h.default_income_currency_code, h.default_spending_currency_code,
		       h.default_display_currency_code,
		       u.default_income_currency_code, u.default_spending_currency_code,
		       u.default_display_currency_code,
		       COALESCE(u.default_income_currency_code, h.default_income_currency_code),
		       COALESCE(u.default_spending_currency_code, h.default_spending_currency_code),
		       COALESCE(u.default_display_currency_code, h.default_display_currency_code)
		FROM household_options h
		JOIN household_memberships m
		  ON m.household_id = h.household_id AND m.user_id = $2
		LEFT JOIN user_options u
		  ON u.household_id = h.household_id AND u.user_id = $2
		WHERE h.household_id = $1`,
		householdID, userID,
	).Scan(
		&view.Household.DefaultIncomeCurrencyCode,
		&view.Household.DefaultSpendingCurrencyCode,
		&view.Household.DefaultDisplayCurrencyCode,
		&view.User.DefaultIncomeCurrencyCode,
		&view.User.DefaultSpendingCurrencyCode,
		&view.User.DefaultDisplayCurrencyCode,
		&view.Effective.DefaultIncomeCurrencyCode,
		&view.Effective.DefaultSpendingCurrencyCode,
		&view.Effective.DefaultDisplayCurrencyCode,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return OptionsView{}, ErrNotFound
	}
	if err != nil {
		return OptionsView{}, fmt.Errorf("get options view: %w", err)
	}
	return view, nil
}

// SetUserOptions replaces personal overrides for a household member. The
// mutation and the returned options are assembled in one transaction, so the
// response matches the values committed by this call.
func (s *Service) SetUserOptions(
	ctx context.Context,
	userID, householdID uuid.UUID,
	params UpdateUserOptionsParams,
) (OptionsView, error) {
	if _, err := s.membershipRole(ctx, householdID, userID); err != nil {
		return OptionsView{}, err
	}

	validation := newOptionsValidation()
	income, err := s.validateOptionalCurrency(ctx, params.DefaultIncomeCurrencyCode, "default_income_currency_code", validation)
	if err != nil {
		return OptionsView{}, err
	}
	spending, err := s.validateOptionalCurrency(ctx, params.DefaultSpendingCurrencyCode, "default_spending_currency_code", validation)
	if err != nil {
		return OptionsView{}, err
	}
	display, err := s.validateOptionalCurrency(ctx, params.DefaultDisplayCurrencyCode, "default_display_currency_code", validation)
	if err != nil {
		return OptionsView{}, err
	}
	if err := validation.error(); err != nil {
		return OptionsView{}, err
	}

	var view OptionsView
	err = store.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_options
				(household_id, user_id, default_income_currency_code,
				 default_spending_currency_code, default_display_currency_code)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (household_id, user_id) DO UPDATE
			SET default_income_currency_code = EXCLUDED.default_income_currency_code,
			    default_spending_currency_code = EXCLUDED.default_spending_currency_code,
			    default_display_currency_code = EXCLUDED.default_display_currency_code,
			    updated_at = now()`,
			householdID, userID, income, spending, display,
		); err != nil {
			return fmt.Errorf("set user options: %w", err)
		}

		var err error
		view, err = s.optionsView(ctx, tx, userID, householdID)
		return err
	})
	if err != nil {
		return OptionsView{}, err
	}
	return view, nil
}

type optionsValidation struct {
	fields map[string]string
}

func newOptionsValidation() *optionsValidation {
	return &optionsValidation{fields: make(map[string]string)}
}

func (v *optionsValidation) add(field, message string) {
	if _, exists := v.fields[field]; !exists {
		v.fields[field] = message
	}
}

func (v *optionsValidation) error() error {
	if len(v.fields) == 0 {
		return nil
	}
	return &OptionsValidationError{Fields: v.fields}
}

// OptionsValidationError describes invalid currency option fields.
type OptionsValidationError struct {
	Fields map[string]string
}

// Error implements error.
func (e *OptionsValidationError) Error() string {
	return "invalid options"
}

func (s *Service) validateCurrency(
	ctx context.Context,
	raw, field string,
	validation *optionsValidation,
) (string, error) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if code == "" {
		validation.add(field, "Choose a currency.")
		return "", nil
	}
	exists, err := currencyExists(ctx, s.pool, code)
	if err != nil {
		return "", err
	}
	if !exists {
		validation.add(field, "Choose a supported currency.")
	}
	return code, nil
}

func (s *Service) validateOptionalCurrency(
	ctx context.Context,
	raw *string,
	field string,
	validation *optionsValidation,
) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	code := strings.ToUpper(strings.TrimSpace(*raw))
	if code == "" {
		return nil, nil
	}
	exists, err := currencyExists(ctx, s.pool, code)
	if err != nil {
		return nil, err
	}
	if !exists {
		validation.add(field, "Choose a supported currency.")
	}
	return &code, nil
}

// currencyExists reports whether the shared catalog contains code.
func currencyExists(ctx context.Context, db rowQuerier, code string) (bool, error) {
	var exists bool
	if err := db.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM currencies WHERE code = $1)", code,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check currency: %w", err)
	}
	return exists, nil
}
