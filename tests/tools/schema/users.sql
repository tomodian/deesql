CREATE TABLE users (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    email      VARCHAR(255) NOT NULL,
    name       TEXT NOT NULL,
    is_active  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_email_unique UNIQUE (email)
);

CREATE INDEX ASYNC idx_users_email ON users (email);
