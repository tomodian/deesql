-- Settings: demonstrates char types, composite primary key, and DEFAULT expressions.

CREATE TABLE settings (
    scope       CHAR(10) NOT NULL,
    key         VARCHAR(255) NOT NULL,
    value       TEXT NOT NULL DEFAULT '',
    description TEXT,
    updated_by  TEXT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, key)
);

CREATE INDEX ASYNC idx_settings_updated ON settings (updated_at);
