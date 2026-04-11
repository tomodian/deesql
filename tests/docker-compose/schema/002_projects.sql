-- Projects table: demonstrates UUID primary key, CHECK constraints, and NULLS ordering.

CREATE TABLE projects (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    name        TEXT NOT NULL,
    owner_id    TEXT NOT NULL,
    description TEXT,
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived', 'deleted')),
    budget      NUMERIC(12,2) CHECK (budget >= 0),
    archived    BOOLEAN NOT NULL DEFAULT FALSE,
    started_at  DATE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ASYNC idx_projects_owner ON projects (owner_id);
CREATE INDEX ASYNC idx_projects_status ON projects (status);
CREATE INDEX ASYNC idx_projects_started ON projects (started_at NULLS LAST);
