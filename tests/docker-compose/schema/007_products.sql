-- Products: demonstrates real/double precision, UNIQUE index, and LIKE clause.

CREATE TABLE products (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    sku         VARCHAR(100) NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT,
    price       NUMERIC(10,2) NOT NULL CHECK (price >= 0),
    weight_kg   REAL,
    volume_cm3  DOUBLE PRECISION,
    in_stock    BOOLEAN NOT NULL DEFAULT TRUE,
    tags        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX ASYNC idx_products_sku ON products (sku);
CREATE INDEX ASYNC idx_products_price ON products (price);
CREATE INDEX ASYNC idx_products_stock ON products (in_stock) INCLUDE (name, price);
