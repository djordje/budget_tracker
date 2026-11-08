-- Income and spending stay in separate tables for clear semantics. Amounts
-- are positive integer minor units; the table determines the direction.
CREATE TABLE incomes (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    amount_minor bigint NOT NULL,
    currency_code text NOT NULL REFERENCES currencies (code) ON UPDATE CASCADE ON DELETE RESTRICT,
    entry_date date NOT NULL,
    description text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT incomes_amount_positive CHECK (amount_minor > 0),
    CONSTRAINT incomes_description_length CHECK (description IS NULL OR char_length(description) <= 500)
);

CREATE INDEX incomes_household_entry_date_idx ON incomes (household_id, entry_date);
CREATE INDEX incomes_household_user_entry_date_idx ON incomes (household_id, user_id, entry_date);

CREATE TABLE spendings (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    household_id uuid NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    amount_minor bigint NOT NULL,
    currency_code text NOT NULL REFERENCES currencies (code) ON UPDATE CASCADE ON DELETE RESTRICT,
    entry_date date NOT NULL,
    description text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT spendings_amount_positive CHECK (amount_minor > 0),
    CONSTRAINT spendings_description_length CHECK (description IS NULL OR char_length(description) <= 500)
);

CREATE INDEX spendings_household_entry_date_idx ON spendings (household_id, entry_date);
CREATE INDEX spendings_household_user_entry_date_idx ON spendings (household_id, user_id, entry_date);
