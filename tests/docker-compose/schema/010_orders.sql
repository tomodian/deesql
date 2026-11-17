-- Orders: demonstrates DSQL features added in 2026 — sequences (explicit CACHE),
-- bigint identity with CACHE, foreign keys, stored generated columns, JSONB,
-- expression / partial / NULLS NOT DISTINCT indexes, and extended statistics.

CREATE SEQUENCE order_number_seq CACHE 65536;

CREATE TABLE orders (
    id         BIGINT GENERATED ALWAYS AS IDENTITY (CACHE 65536) PRIMARY KEY,
    number     BIGINT NOT NULL DEFAULT nextval('order_number_seq'),
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    project_id TEXT,
    status     TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'paid', 'cancelled')),
    qty        INTEGER NOT NULL CHECK (qty > 0),
    unit_price NUMERIC(10,2) NOT NULL,
    total      NUMERIC(12,2) GENERATED ALWAYS AS (qty * unit_price) STORED,
    metadata   JSONB,
    CONSTRAINT orders_project_fk FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE SET NULL
);

CREATE INDEX ASYNC idx_orders_user ON orders (user_id);
CREATE INDEX ASYNC idx_orders_open ON orders (user_id, status) WHERE status = 'open';
CREATE INDEX ASYNC idx_orders_lower_status ON orders ((lower(status)));
CREATE UNIQUE INDEX ASYNC idx_orders_number ON orders (number) NULLS NOT DISTINCT;

CREATE STATISTICS orders_user_status (ndistinct, dependencies) ON user_id, status FROM orders;
