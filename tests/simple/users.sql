CREATE TABLE users (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    email      TEXT NOT NULL UNIQUE,
    name       TEXT NULL,
    phone      TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ASYNC idx_users_created_at ON users (created_at);
