-- Households and household-specific roles.
CREATE TABLE households (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT households_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT households_name_length CHECK (char_length(btrim(name)) <= 100)
);

CREATE TABLE household_memberships (
    household_id uuid NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT household_memberships_role_valid CHECK (role IN ('admin', 'member')),
    PRIMARY KEY (household_id, user_id)
);

CREATE INDEX household_memberships_user_id_idx ON household_memberships (user_id);

-- Currency codes are constrained now and will become foreign keys when the
-- currencies table is introduced with the finance milestone.
CREATE TABLE household_options (
    household_id uuid PRIMARY KEY REFERENCES households (id) ON DELETE CASCADE,
    default_income_currency_code text NOT NULL DEFAULT 'RSD',
    default_spending_currency_code text NOT NULL DEFAULT 'RSD',
    default_display_currency_code text NOT NULL DEFAULT 'RSD',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT household_options_income_currency_valid CHECK (default_income_currency_code ~ '^[A-Z]{3}$'),
    CONSTRAINT household_options_spending_currency_valid CHECK (default_spending_currency_code ~ '^[A-Z]{3}$'),
    CONSTRAINT household_options_display_currency_valid CHECK (default_display_currency_code ~ '^[A-Z]{3}$')
);

-- The active household is a session preference, not an authorization rule.
ALTER TABLE sessions
    ADD COLUMN active_household_id uuid REFERENCES households (id) ON DELETE SET NULL;

CREATE INDEX sessions_active_household_id_idx ON sessions (active_household_id);
