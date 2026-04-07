CREATE TABLE tasks (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    project_id  TEXT NOT NULL,
    assignee_id TEXT,
    title       TEXT NOT NULL,
    description TEXT,
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'done')),
    priority    INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ASYNC idx_tasks_project_id ON tasks (project_id);
CREATE INDEX ASYNC idx_tasks_assignee_id ON tasks (assignee_id);
CREATE INDEX ASYNC idx_tasks_status ON tasks (status);
