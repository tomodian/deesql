-- Tasks table: demonstrates composite unique constraints, INCLUDE indexes, and GENERATED STORED columns.

CREATE TABLE tasks (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    project_id  TEXT NOT NULL,
    assignee_id TEXT,
    title       TEXT NOT NULL,
    description TEXT,
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'review', 'done')),
    priority    SMALLINT NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 4),
    due_date    DATE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ASYNC idx_tasks_project ON tasks (project_id);
CREATE INDEX ASYNC idx_tasks_assignee ON tasks (assignee_id);
CREATE INDEX ASYNC idx_tasks_status ON tasks (status);
CREATE INDEX ASYNC idx_tasks_due ON tasks (due_date NULLS LAST);

-- Covering index: include title for index-only scans
CREATE INDEX ASYNC idx_tasks_project_status ON tasks (project_id, status) INCLUDE (title);
