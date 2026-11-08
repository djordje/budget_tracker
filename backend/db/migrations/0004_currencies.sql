-- ISO 4217 currencies used by entries and exchange rates. minor_units controls
-- storage conversion and display precision; it is not always two.
CREATE TABLE currencies (
    code text PRIMARY KEY,
    numeric_code integer NOT NULL,
    name text NOT NULL,
    minor_units smallint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT currencies_code_format CHECK (code ~ '^[A-Z]{3}$'),
    CONSTRAINT currencies_numeric_code_range CHECK (numeric_code BETWEEN 1 AND 999),
    CONSTRAINT currencies_numeric_code_unique UNIQUE (numeric_code),
    CONSTRAINT currencies_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT currencies_minor_units_range CHECK (minor_units BETWEEN 0 AND 4)
);

INSERT INTO currencies (code, numeric_code, name, minor_units) VALUES
    ('RSD', 941, 'Serbian dinar', 2),
    ('EUR', 978, 'Euro', 2),
    ('USD', 840, 'US dollar', 2),
    ('GBP', 826, 'Pound sterling', 2),
    ('CHF', 756, 'Swiss franc', 2),
    ('BAM', 977, 'Convertible mark', 2),
    ('CAD', 124, 'Canadian dollar', 2),
    ('AUD', 036, 'Australian dollar', 2),
    ('JPY', 392, 'Japanese yen', 0)
ON CONFLICT (code) DO NOTHING;

ALTER TABLE household_options
    ADD CONSTRAINT household_options_income_currency_fk
        FOREIGN KEY (default_income_currency_code) REFERENCES currencies (code)
        ON UPDATE CASCADE ON DELETE RESTRICT,
    ADD CONSTRAINT household_options_spending_currency_fk
        FOREIGN KEY (default_spending_currency_code) REFERENCES currencies (code)
        ON UPDATE CASCADE ON DELETE RESTRICT,
    ADD CONSTRAINT household_options_display_currency_fk
        FOREIGN KEY (default_display_currency_code) REFERENCES currencies (code)
        ON UPDATE CASCADE ON DELETE RESTRICT;
