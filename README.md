# dsql-migrate

Schema migration tool for [Amazon Aurora DSQL](https://aws.amazon.com/rds/aurora/dsql/).

Compares your desired schema (`.sql` files) against a live Aurora DSQL cluster and generates a migration plan. No migration history table, no temp databases -- just declarative schema diffing.

## Install

### Go

```sh
go install tomodian/dsql-migrate@latest
```

### Docker

```sh
docker run --rm ghcr.io/tomodian/dsql-migrate:latest --help
```

To run with AWS credentials:

```sh
docker run --rm \
  -v ~/.aws:/home/nonroot/.aws:ro \
  -e AWS_PROFILE \
  -v $(pwd)/schema:/schema:ro \
  ghcr.io/tomodian/dsql-migrate:latest \
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
dsql-migrate verify --schema ./schema
```

3. Preview changes:

```sh
dsql-migrate plan --endpoint <cluster>.dsql.<region>.on.aws --schema ./schema
```

4. Apply:

```sh
dsql-migrate apply --endpoint <cluster>.dsql.<region>.on.aws --schema ./schema
```

## How It Works

```
.sql files ──parse──> Desired Schema ──┐
                                       ├──diff──> Migration Plan ──apply──> DSQL
Live DSQL  ──introspect──> Current Schema──┘
```

1. **Parse** `.sql` files into a schema model (no SQL execution, no temp database)
2. **Introspect** the live DSQL cluster via `pg_catalog`
3. **Diff** the two schemas and generate ordered DDL statements
4. **Apply** each statement to the live cluster

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
dsql-migrate plan --endpoint <endpoint> --schema ./schema
```

### `apply`

Generate and apply a migration plan.

```sh
dsql-migrate apply --endpoint <endpoint> --schema ./schema [--skip-confirm] [--allow-hazards DELETES_DATA,INDEX_BUILD]
```

### `verify`

Check schema files for Aurora DSQL compatibility (no database connection needed).

```sh
dsql-migrate verify --schema ./schema
```

## Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--endpoint` | Aurora DSQL cluster endpoint | (required for plan/apply) |
| `--region` | AWS region | auto-detected from endpoint |
| `--user` | Database user | `admin` |
| `--schema` | Directories with `.sql` files (repeatable) | (required) |
| `--profile` | AWS profile name | `$AWS_PROFILE` |
| `--role-arn` | IAM role ARN to assume | (none) |
| `--connect-timeout` | Connection timeout | `10s` |
| `--allow-hazards` | Hazard types to permit (apply only) | (none) |
| `--skip-confirm` | Skip confirmation prompt (apply only) | `false` |

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
| Drop column | Yes |
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
dsql-migrate apply --endpoint <endpoint> --schema ./schema --allow-hazards DELETES_DATA,INDEX_BUILD
```

## Design Principles

- **Stateless** -- No migration history table. The plan is always computed fresh from the diff between desired and live schemas.
- **No temp database** -- SQL files are parsed in-process. No need for a secondary PostgreSQL or DSQL cluster.
- **DSQL-native** -- Built specifically for Aurora DSQL's constraints (async indexes, IAM auth, single-DDL transactions, limited ALTER TABLE).
- **Safe by default** -- Hazardous operations require explicit opt-in via `--allow-hazards`.

## License

MIT
