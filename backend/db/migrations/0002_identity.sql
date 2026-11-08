-- Public user profiles. Authentication secrets live in auth_credentials.
CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    first_name text NOT NULL CHECK (btrim(first_name) <> ''),
    last_name text NOT NULL CHECK (btrim(last_name) <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Login credentials are isolated from the public profile so authentication
-- can be replaced without changing household or finance tables.
CREATE TABLE auth_credentials (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    email_normalized text NOT NULL,
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT auth_credentials_email_normalized_unique UNIQUE (email_normalized),
    CONSTRAINT auth_credentials_email_normalized_canonical
        CHECK (email_normalized = lower(btrim(email_normalized)))
);

-- Opaque session tokens are hashed at rest and the hash is the session
-- identity. Sessions carry expiry and can be revoked individually.
CREATE TABLE sessions (
    token_hash text PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sessions_token_hash_length CHECK (char_length(token_hash) = 64)
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);
