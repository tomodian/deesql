-- v2: drops is_active (DROP COLUMN), loosens name (DROP NOT NULL), changes a default.
CREATE TABLE users (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    email      VARCHAR(255) NOT NULL,
    name       TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT users_email_unique UNIQUE (email)
);

CREATE INDEX ASYNC idx_users_email ON users (email);
