[![test](https://github.com/tomodian/deesql/actions/workflows/test.yml/badge.svg)](https://github.com/tomodian/deesql/actions/workflows/test.yml) [![docker](https://github.com/tomodian/deesql/actions/workflows/docker.yml/badge.svg)](https://github.com/tomodian/deesql/actions/workflows/docker.yml)

# deesql

The missing toolkit for [Amazon Aurora DSQL](https://aws.amazon.com/rds/aurora/dsql/) -- schema migrations, compatibility checking, and a local development proxy, all in a single binary.

deesql compares your desired schema (`.sql` files) against a live Aurora DSQL cluster and generates a migration plan. It also ships a local proxy server that lets you develop against a standard PostgreSQL container while enforcing DSQL compatibility at the wire protocol level.

No migration history table, no temp databases -- just declarative schema diffing and a DSQL-native development workflow.

## Why?

[Aurora DSQL](https://aws.amazon.com/rds/aurora/dsql/) is a fantastic choice for building modern applications. It gives you a serverless, virtually unlimited, active-active distributed SQL database with strong consistency -- all at an affordable pay-per-request price point. And because it speaks the PostgreSQL wire protocol, your existing tools, drivers, and ORMs just work.

The one catch: DSQL supports a subset of PostgreSQL. Features like `CREATE EXTENSION`, triggers, PL/pgSQL, and `FOREIGN KEY` constraints aren't available. This is a reasonable tradeoff for the scalability and simplicity DSQL provides, but it means existing migration tools (designed for full PostgreSQL) can generate SQL that DSQL rejects at apply time.

deesql bridges that gap:

- **`plan` / `apply`** -- Stateless, declarative migrations built specifically for DSQL. Parses your `.sql` files, introspects the live cluster, diffs, and applies -- respecting DSQL's DDL constraints at every step.
- **`verify`** -- Catches DSQL-incompatible SQL in your schema files before you ever connect to a cluster.
- **`proxy`** -- A local TCP proxy that sits between your app and a standard PostgreSQL container, intercepting and rejecting unsupported SQL with real DSQL error codes. Develop locally with full confidence that your SQL will work on DSQL.

## Comparison

| Feature | deesql | Atlas | Flyway |
|---------|--------|-------|--------|
| Aurora DSQL support | First-class | Pro Plan required | Generic PostgreSQL |
| Migration approach | Declarative (desired-state diffing) | Declarative + versioned | Versioned (sequential migrations) |
| Migration history table | None (stateless) | Required (`atlas_schema_revisions`) | Required (`flyway_schema_history`) |
| DSQL compatibility checking | Built-in (`verify` command) | Pro Plan required | No |
| Local DSQL proxy | Built-in (`proxy` command) | No | No |
| `CREATE INDEX ASYNC` | Native support | No awareness | No awareness |
| 1 DDL per transaction | Handled automatically | Manual workaround | Manual workaround |
| IAM authentication | Built-in (AWS SDK chain) | Manual DSN config | Manual DSN config |
| Unsupported ALTER TABLE detection | Errors at plan time | Errors at apply time | Errors at apply time |
| Temp database required | No (in-process parsing) | Yes (for some providers) | No |
| Language | Go (single binary) | Go (single binary) | Java (JVM required) |

## Install

### Go

```sh
go install tomodian/deesql@latest
```

### Docker

```sh
docker run --rm ghcr.io/tomodian/deesql:latest --help
```

To run with AWS credentials:

```sh
docker run --rm \
  -v ~/.aws:/home/nonroot/.aws:ro \
  -e AWS_PROFILE \
  -v $(pwd)/schema:/schema:ro \
  ghcr.io/tomodian/deesql:latest \
  plan --endpoint <cluster>.dsql.<region>.on.aws --schema /schema
```

### Build from source

```sh
git clone <repo>
cd migrate
make build
```

## Quick Start

1. Define your desired schema in `.sql` files:

```sql
-- schema/users.sql
CREATE TABLE users (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    email      TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ASYNC idx_users_email ON users (email);
```

2. Check compatibility:

```sh
deesql verify --schema ./schema
```

3. Preview changes:

```sh
deesql plan --endpoint <cluster>.dsql.<region>.on.aws --schema ./schema
```

4. Apply:

```sh
deesql apply --endpoint <cluster>.dsql.<region>.on.aws --schema ./schema
```

5. Develop locally with the DSQL proxy:

```sh
# Start a PostgreSQL container
docker run -d --name pg -p 5432:5432 -e POSTGRES_HOST_AUTH_METHOD=trust postgres:latest

# Start the proxy
deesql proxy --listen :15432 --upstream localhost:5432

# Connect through the proxy -- unsupported SQL is rejected with DSQL error codes
psql -h localhost -p 15432 -U postgres
```

## How It Works

### Schema Migrations

```
.sql files ──parse──> Desired Schema ──┐
                                       ├──diff──> Migration Plan ──apply──> DSQL
Live DSQL  ──introspect──> Current Schema──┘
```

1. **Parse** `.sql` files into a schema model (no SQL execution, no temp database)
2. **Introspect** the live DSQL cluster via `pg_catalog`
3. **Diff** the two schemas and generate ordered DDL statements
4. **Apply** each statement to the live cluster

### Local Proxy

```
App / psql ──> deesql proxy (:15432) ──> PostgreSQL (:5432)
                    │
               Intercepts SQL
               Blocks unsupported operations
               Returns DSQL-compatible errors (SQLSTATE 0A000)
```

The proxy speaks the PostgreSQL wire protocol, inspecting `Query` and `Parse` messages. Unsupported SQL is rejected immediately with the same error codes Aurora DSQL would return, while allowed SQL is forwarded to the backend.

## Output

Plans use Terraform-style change indicators:

```
Migration plan (3 statement(s)):
------------------------------------------------------------

  + table.orders
    -- Statement 1
    CREATE TABLE orders (...);

  ~ table.users
    -- Statement 2
    ALTER TABLE users ADD COLUMN bio TEXT;

  + index.idx_orders_user_id
    -- Statement 3
    CREATE INDEX ASYNC idx_orders_user_id ON orders (user_id);
    -- ⚠ INDEX_BUILD: Building index idx_orders_user_id asynchronously

------------------------------------------------------------
Plan: 2 to create, 1 to update.
```

| Symbol | Action | Description |
|--------|--------|-------------|
| `+` | Create | A new resource will be provisioned |
| `~` | Update | An existing resource will be modified in place |
| `-` | Destroy | An existing resource will be deleted |
| `+/-` | Replace | A resource will be destroyed and recreated |

## Commands

### `plan`

Generate and display a migration plan without applying it.

```sh
deesql plan --endpoint <endpoint> --schema ./schema
```

### `apply`

Generate and apply a migration plan.

```sh
deesql apply --endpoint <endpoint> --schema ./schema [--force] [--allow-hazards DELETES_DATA,INDEX_BUILD]
```

### `verify`

Check schema files for Aurora DSQL compatibility (no database connection needed).

```sh
deesql verify --schema ./schema
```

### `proxy`

Start a DSQL-filtering proxy between a PostgreSQL client and backend.

```sh
deesql proxy [--listen :15432] [--upstream localhost:5432]
```

The proxy intercepts and blocks 35+ unsupported SQL patterns including:

- Unsupported DDL: `CREATE DATABASE`, `CREATE EXTENSION`, `CREATE TRIGGER`, `CREATE TYPE`, `CREATE PROCEDURE`, `CREATE RULE`, `CREATE UNLOGGED TABLE`, `CREATE MATERIALIZED VIEW`, `CREATE TABLE AS SELECT`
- Table restrictions: `INHERITS`, `PARTITION BY`, `COLLATE`, `FOREIGN KEY`, `EXCLUDE`
- Index restrictions: synchronous `CREATE INDEX` (must use `ASYNC`), `CONCURRENTLY`, non-btree types, `ASC`/`DESC` ordering
- Unsupported statements: `TRUNCATE`, `ALTER SYSTEM`, `VACUUM`, `SAVEPOINT`, `LISTEN`/`NOTIFY`, `LOCK TABLE`
- Function restrictions: non-SQL languages (`plpgsql`, `plv8`, etc.)

## Flags

### Global

| Flag | Description | Default |
|------|-------------|---------|
| `--endpoint` | Aurora DSQL cluster endpoint | (required for plan/apply) |
| `--region` | AWS region | auto-detected from endpoint |
| `--user` | Database user | `admin` |
| `--schema` | Directories with `.sql` files (repeatable) | (required) |
| `--profile` | AWS profile name | `$AWS_PROFILE` |
| `--role-arn` | IAM role ARN to assume | (none) |
| `--connect-timeout` | Connection timeout | `10s` |

### apply

| Flag | Description | Default |
|------|-------------|---------|
| `--allow-hazards` | Hazard types to permit | (none) |
| `--force` | Skip confirmation prompt | `false` |

### proxy

| Flag | Description | Default |
|------|-------------|---------|
| `--listen` | Address to listen on | `:15432` |
| `--upstream` | Backend PostgreSQL address | `localhost:5432` |

## AWS Authentication

Credentials are resolved via the standard [AWS SDK default credential chain](https://docs.aws.amazon.com/sdkref/latest/guide/standardized-credentials.html):

1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`)
2. Shared credentials file (`~/.aws/credentials`)
3. AWS config file with `--profile` or `$AWS_PROFILE`
4. IAM role assumption with `--role-arn` (via STS)
5. EC2/ECS instance metadata (IMDS)

## Supported DDL

### Schema file syntax

| Feature | Syntax | Notes |
|---------|--------|-------|
| Create table | `CREATE TABLE name (...)` | With columns, inline constraints |
| Create index | `CREATE [UNIQUE] INDEX ASYNC name ON table (cols)` | DSQL requires `ASYNC` |
| Primary key | `PRIMARY KEY (cols)` | Inline or table-level |
| Unique constraint | `UNIQUE (cols)` | Inline or table-level |
| Check constraint | `CHECK (expr)` | Inline or table-level |
| Identity column | `GENERATED {ALWAYS\|BY DEFAULT} AS IDENTITY` | Not `SERIAL` |
| Default value | `DEFAULT expr` | Literals or functions (`now()`, `gen_random_uuid()`) |

### Supported column types

| Category | Types |
|----------|-------|
| Text | `TEXT`, `VARCHAR(N)`, `CHAR(N)` |
| Numeric | `INTEGER`, `BIGINT`, `SMALLINT`, `REAL`, `DOUBLE PRECISION`, `NUMERIC` |
| Boolean | `BOOLEAN` |
| Date/Time | `TIMESTAMPTZ`, `TIMESTAMP`, `DATE`, `TIME`, `INTERVAL` |
| Binary | `BYTEA` |
| Other | `UUID` |

### Migration operations

| Operation | Supported |
|-----------|-----------|
| Create table | Yes |
| Drop table | Yes |
| Add column | Yes |
| Drop column | No (DSQL limitation) |
| Add/drop index | Yes (`CREATE INDEX ASYNC`) |
| Add/drop check constraint | Yes |
| Add/drop unique constraint | Yes |
| Change column type | No (DSQL limitation) |
| Change NOT NULL | No (DSQL limitation) |
| Change default | No (DSQL limitation) |
| Change primary key | No (requires table recreation) |

## Hazard Types

Hazards warn about potentially dangerous operations:

| Type | Description |
|------|-------------|
| `DELETES_DATA` | Drops a table or column and its data |
| `INDEX_BUILD` | Async index build in progress |
| `INDEX_DROPPED` | Dropping an index may degrade query performance |

Use `--allow-hazards` to permit specific types:

```sh
deesql apply --endpoint <endpoint> --schema ./schema --allow-hazards DELETES_DATA,INDEX_BUILD
```

## Design Principles

- **Stateless** -- No migration history table. The plan is always computed fresh from the diff between desired and live schemas.
- **No temp database** -- SQL files are parsed in-process. No need for a secondary PostgreSQL or DSQL cluster.
- **DSQL-native** -- Built to complement Aurora DSQL's strengths (async indexes, IAM auth, single-DDL transactions) so you can focus on your application, not migration plumbing.
- **Safe by default** -- Hazardous operations require explicit opt-in via `--allow-hazards`.
- **Local-first** -- The proxy brings DSQL's behavior to your local PostgreSQL, so you develop with confidence from day one.

## License

MIT
