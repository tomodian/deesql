CREATE TABLE projects (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    name       TEXT NOT NULL,
    owner_id   TEXT NOT NULL,
    archived   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ASYNC idx_projects_owner_id ON projects (owner_id);
