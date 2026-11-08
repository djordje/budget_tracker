-- Household-specific exact decimal rates. Monthly rows normalize rate_date to
-- the first day of their month; exact-date rows apply only on that date.
CREATE TABLE exchange_rates (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    from_currency_code text NOT NULL REFERENCES currencies (code) ON UPDATE CASCADE ON DELETE RESTRICT,
    to_currency_code text NOT NULL REFERENCES currencies (code) ON UPDATE CASCADE ON DELETE RESTRICT,
    rate numeric(18, 8) NOT NULL,
    rate_date date NOT NULL,
    is_monthly boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT exchange_rates_rate_positive CHECK (rate > 0),
    CONSTRAINT exchange_rates_currencies_differ CHECK (from_currency_code <> to_currency_code),
    CONSTRAINT exchange_rates_monthly_date_normalized
        CHECK (NOT is_monthly OR rate_date = date_trunc('month', rate_date)::date)
);

CREATE UNIQUE INDEX exchange_rates_exact_unique
    ON exchange_rates (household_id, from_currency_code, to_currency_code, rate_date)
    WHERE NOT is_monthly;

CREATE UNIQUE INDEX exchange_rates_monthly_unique
    ON exchange_rates (household_id, from_currency_code, to_currency_code, rate_date)
    WHERE is_monthly;

CREATE INDEX exchange_rates_household_idx ON exchange_rates (household_id);
