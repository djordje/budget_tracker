-- Invitations are single-use and store only a hash of the link token. The
-- accepting account's normalized email must match email_normalized.
CREATE TABLE household_invitations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    email_normalized text NOT NULL,
    role text NOT NULL,
    invited_by_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash text NOT NULL,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT household_invitations_email_canonical
        CHECK (email_normalized = lower(btrim(email_normalized))),
    CONSTRAINT household_invitations_role_valid CHECK (role IN ('admin', 'member')),
    CONSTRAINT household_invitations_token_hash_unique UNIQUE (token_hash),
    CONSTRAINT household_invitations_token_hash_length CHECK (char_length(token_hash) = 64)
);

CREATE INDEX household_invitations_household_idx ON household_invitations (household_id);

CREATE UNIQUE INDEX household_invitations_pending_unique
    ON household_invitations (household_id, email_normalized)
    WHERE accepted_at IS NULL;
