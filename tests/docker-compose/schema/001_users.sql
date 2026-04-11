-- Users table: demonstrates all supported column types and constraints.

CREATE TABLE users (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    email       VARCHAR(255) NOT NULL,
    name        TEXT NOT NULL,
    phone       VARCHAR(20),
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    login_count INTEGER NOT NULL DEFAULT 0,
    rating      NUMERIC(5,2),
    avatar      BYTEA,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_email_unique UNIQUE (email),
    CONSTRAINT users_rating_check CHECK (rating >= 0 AND rating <= 5)
);

CREATE INDEX ASYNC idx_users_email ON users (email);
CREATE INDEX ASYNC idx_users_created_at ON users (created_at);
CREATE INDEX ASYNC idx_users_active ON users (is_active);
