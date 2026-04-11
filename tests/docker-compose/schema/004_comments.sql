-- Comments table: demonstrates all numeric types and NULLS NOT DISTINCT unique constraint.

CREATE TABLE comments (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    task_id     TEXT NOT NULL,
    author_id   TEXT NOT NULL,
    body        TEXT NOT NULL,
    parent_id   TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Prevent duplicate (task_id, author_id, body) including NULLs
    CONSTRAINT comments_no_dup UNIQUE (task_id, author_id, body)
);

CREATE INDEX ASYNC idx_comments_task ON comments (task_id);
CREATE INDEX ASYNC idx_comments_author ON comments (author_id);
