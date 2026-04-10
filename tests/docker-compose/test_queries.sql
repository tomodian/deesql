-- Test queries: run through the deesql proxy to verify DSQL-compatible SQL works.
-- Usage: psql -h localhost -p 15432 -U admin -d postgres -f test_queries.sql

-- INSERT
INSERT INTO users (email, name, phone, rating) VALUES ('alice@example.com', 'Alice', '+1-555-0100', 4.5);
INSERT INTO users (email, name, is_active) VALUES ('bob@example.com', 'Bob', FALSE);

INSERT INTO projects (name, owner_id, description, budget, started_at)
VALUES ('Project Alpha', 'owner-1', 'First project', 50000.00, '2026-01-15');

INSERT INTO tasks (project_id, assignee_id, title, status, priority, due_date)
SELECT p.id, 'assignee-1', 'Setup CI/CD', 'open', 2, '2026-05-01'
FROM projects p WHERE p.name = 'Project Alpha';

INSERT INTO settings (scope, key, value) VALUES ('global', 'theme', 'dark');

INSERT INTO products (sku, name, price, weight_kg, volume_cm3, tags)
VALUES ('SKU-001', 'Widget', 29.99, 0.5, 120.75, 'electronics,gadgets');

-- UPDATE with FROM
UPDATE tasks SET assignee_id = u.id
FROM users u
WHERE u.email = 'alice@example.com' AND tasks.assignee_id IS NULL;

-- SELECT with JOINs, CTEs, window functions
WITH task_counts AS (
    SELECT project_id, COUNT(*) AS total
    FROM tasks
    GROUP BY project_id
)
SELECT p.name, COALESCE(tc.total, 0) AS tasks
FROM projects p
LEFT JOIN task_counts tc ON tc.project_id = p.id;

SELECT
    t.title,
    t.priority,
    RANK() OVER (PARTITION BY t.project_id ORDER BY t.priority DESC) AS priority_rank
FROM tasks t;

-- DELETE with USING
DELETE FROM comments c
USING tasks t
WHERE c.task_id = t.id AND t.status = 'deleted';

-- JSON at query time (store as text, cast to json)
SELECT json_build_object('user', u.name, 'email', u.email)::text AS user_json
FROM users u
LIMIT 1;

-- Array at query time
SELECT string_to_array(p.tags, ',') AS tag_array
FROM products p
LIMIT 1;

-- UNION / INTERSECT / EXCEPT
SELECT name, 'user' AS type FROM users
UNION ALL
SELECT name, 'project' AS type FROM projects;

-- ANALYZE (must specify table name on DSQL)
ANALYZE users;

-- Verify everything
SELECT 'All test queries passed' AS result;
