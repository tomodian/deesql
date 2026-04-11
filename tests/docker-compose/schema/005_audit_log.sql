-- Audit log: demonstrates all supported date/time types and wide rows.

CREATE TABLE audit_log (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    actor_id    TEXT NOT NULL,
    action      VARCHAR(50) NOT NULL CHECK (action IN ('create', 'update', 'delete', 'login', 'logout')),
    resource    VARCHAR(100) NOT NULL,
    resource_id TEXT NOT NULL,
    detail      TEXT,
    ip_address  VARCHAR(45),
    duration    INTERVAL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    event_date  DATE NOT NULL DEFAULT CURRENT_DATE,
    event_time  TIME NOT NULL DEFAULT CURRENT_TIME
);

CREATE INDEX ASYNC idx_audit_actor ON audit_log (actor_id);
CREATE INDEX ASYNC idx_audit_resource ON audit_log (resource, resource_id);
CREATE INDEX ASYNC idx_audit_occurred ON audit_log (occurred_at);
CREATE INDEX ASYNC idx_audit_action_date ON audit_log (action, event_date);
