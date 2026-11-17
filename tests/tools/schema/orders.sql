-- Exercises DSQL features added in 2026: sequences, identity CACHE, foreign keys,
-- stored generated columns, JSONB, expression/partial/NULLS NOT DISTINCT indexes,
-- and extended statistics.

CREATE SEQUENCE order_number_seq CACHE 65536;
CREATE SEQUENCE legacy_seq CACHE 1;

CREATE TABLE orders (
    id         BIGINT GENERATED ALWAYS AS IDENTITY (CACHE 65536) PRIMARY KEY,
    number     BIGINT NOT NULL DEFAULT nextval('order_number_seq'),
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'open',
    qty        INTEGER NOT NULL,
    unit_price NUMERIC(10,2) NOT NULL,
    total      NUMERIC(12,2) GENERATED ALWAYS AS (qty * unit_price) STORED,
    note       TEXT,
    metadata   JSONB,
    CONSTRAINT orders_qty_positive CHECK (qty > 0)
);

CREATE INDEX ASYNC idx_orders_user ON orders (user_id);
CREATE INDEX ASYNC idx_orders_open ON orders (user_id, status) WHERE status = 'open';
CREATE INDEX ASYNC idx_orders_lower_status ON orders ((lower(status)));
CREATE UNIQUE INDEX ASYNC idx_orders_number ON orders (number) NULLS NOT DISTINCT;

CREATE STATISTICS orders_user_status (ndistinct, dependencies) ON user_id, status FROM orders;
