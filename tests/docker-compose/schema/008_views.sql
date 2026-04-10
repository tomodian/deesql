-- Views: demonstrates CREATE VIEW, RECURSIVE VIEW, and updatable views.

CREATE VIEW active_projects AS
    SELECT id, name, owner_id, budget, started_at, created_at
    FROM projects
    WHERE status = 'active';

CREATE VIEW task_summary AS
    SELECT
        p.id AS project_id,
        p.name AS project_name,
        t.status,
        COUNT(*) AS task_count
    FROM projects p
    INNER JOIN tasks t ON t.project_id = p.id
    GROUP BY p.id, p.name, t.status;

CREATE VIEW open_tasks AS
    SELECT id, project_id, assignee_id, title, priority, due_date
    FROM tasks
    WHERE status = 'open'
    WITH CASCADED CHECK OPTION;
