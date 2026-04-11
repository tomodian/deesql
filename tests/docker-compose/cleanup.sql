-- Cleanup: drop all objects created by the example schema.
-- Each statement runs in its own implicit transaction (DSQL: max 1 DDL per tx).

-- Views first (depend on tables)
DROP VIEW IF EXISTS open_tasks;
DROP VIEW IF EXISTS task_summary;
DROP VIEW IF EXISTS active_projects;

-- Functions
DROP FUNCTION IF EXISTS is_overdue(DATE);
DROP FUNCTION IF EXISTS full_name(TEXT, TEXT);

-- Tables (indexes and constraints are dropped automatically)
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS users;
