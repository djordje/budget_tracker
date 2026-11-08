-- Household-specific user overrides. A NULL currency means inherit the
-- household default.
CREATE TABLE user_options (
    household_id uuid NOT NULL,
    user_id uuid NOT NULL,
    default_income_currency_code text REFERENCES currencies (code) ON UPDATE CASCADE ON DELETE RESTRICT,
    default_spending_currency_code text REFERENCES currencies (code) ON UPDATE CASCADE ON DELETE RESTRICT,
    default_display_currency_code text REFERENCES currencies (code) ON UPDATE CASCADE ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, user_id),
    CONSTRAINT user_options_membership_fk
        FOREIGN KEY (household_id, user_id)
        REFERENCES household_memberships (household_id, user_id) ON DELETE CASCADE
);
