# deesql

CLI tool for schema migrations on Amazon Aurora DSQL.

Go module: `github.com/tomodian/deesql`

## Commands

- `plan` — Connect to DSQL, diff live schema against desired-state `.sql` files, print migration plan.
- `apply` — Same as plan, then execute the migration statements. Retries on OCC conflicts.
- `verify` — Check `.sql` files for DSQL compatibility without connecting to a database.
- `proxy` — Start a DSQL-filtering TCP proxy between a PostgreSQL client and backend that intercepts and blocks unsupported SQL statements.
- `sql` — Execute a raw SQL file against Aurora DSQL (for cleanup, seeding, etc.).

## Architecture

```
migrate/
  main.go                       # Entry point
  cli/
    root.go                     # Root urfave/cli v3 app + shared flags
    consts.go                   # Flag name constants and defaults
    plan.go                     # "plan" subcommand
    apply.go                    # "apply" subcommand
    verify.go                   # "verify" subcommand
    proxy.go                    # "proxy" subcommand
    sql.go                      # "sql" subcommand (raw SQL execution)
    helpers.go                  # Flag extraction, schema resolution, verification
  internal/
    dsqlconn/
      conn.go                   # IAM token generation + *sql.DB creation
    planner/planner.go          # Orchestrates parse → introspect → diff
    runner/runner.go            # Hazard checking + statement execution
    schema/
      model.go                  # Schema, Table, Column, Constraint, Index types
      parse.go                  # Parse .sql files into schema models
      introspect.go             # Read live schema from pg_catalog
      normalize.go              # Type + default + check expression normalization
      diff.go                   # Diff current vs desired, generate DDL plan
      generate.go               # DDL generation helpers
      plan.go                   # Plan, Statement, Action, Hazard types
    output/format.go            # Plan output formatting (Terraform-style summary)
    rules/rules.go              # Shared Rule type, SharedRules (line-level), StatementRules (multi-line), Split()
    verify/verify.go            # Regex-based DSQL compatibility checker (static .sql files)
    proxy/
      server.go                 # TCP listener and graceful shutdown
      handler.go                # Per-connection PG protocol relay (startup, auth, steady-state)
      intercept.go              # SQL interception: Check(), splitStatements(), proxy-specific rules
    ui/ui.go                    # Colored terminal output helpers
  tests/
    simple/                     # Example desired-state schema files
    docker-compose/             # E2E tests: test-docker, test-psql, test-dsql
```

## Dependencies

- `github.com/urfave/cli/v3` — CLI framework
- `github.com/jackc/pgx/v4` — PostgreSQL driver
- `github.com/jackc/pgconn` — low-level PG connection (fallback configs)
- `github.com/go-playground/validator/v10` — struct validation
- `github.com/fatih/color` — colored terminal output
- `github.com/aws/aws-sdk-go-v2`, `config`, `credentials`, `feature/dsql/auth`, `service/sts` — IAM auth and role assumption
- `github.com/jackc/pgproto3/v2` — PostgreSQL wire protocol (frontend/backend message parsing for proxy)
- `github.com/DATA-DOG/go-sqlmock` — SQL mock for testing
- `github.com/stretchr/testify` — test assertions

## Aurora DSQL Constraints

Official reference: [Aurora DSQL supported SQL subsets](https://docs.aws.amazon.com/aurora-dsql/latest/userguide/working-with-postgresql-compatibility-supported-sql-subsets.html) | [Unsupported features](https://docs.aws.amazon.com/aurora-dsql/latest/userguide/working-with-postgresql-compatibility-unsupported-features.html) | [ALTER TABLE syntax](https://docs.aws.amazon.com/aurora-dsql/latest/userguide/alter-table-syntax-support.html) | [CREATE INDEX](https://docs.aws.amazon.com/aurora-dsql/latest/userguide/create-index-syntax-support.html) | [CREATE SEQUENCE](https://docs.aws.amazon.com/aurora-dsql/latest/userguide/create-sequence-syntax-support.html) | [Foreign keys](https://docs.aws.amazon.com/aurora-dsql/latest/userguide/working-with-foreign-key-constraints.html) | [ALTER SYSTEM](https://docs.aws.amazon.com/aurora-dsql/latest/userguide/alter-system-syntax-support.html) | [Release notes](https://docs.aws.amazon.com/aurora-dsql/latest/userguide/release-notes.html)

Last reviewed against the release notes on 2026-11-17 (newest entry: 2026-11-13, ALTER SYSTEM).

- **No `CREATE DATABASE`**: Only the `postgres` database exists per cluster.
- **Indexes**: `CREATE [UNIQUE] INDEX ASYNC` only (btree). Keys may be columns or immutable `(expressions)` with `NULLS FIRST/LAST`; `INCLUDE`, `NULLS [NOT] DISTINCT`, and partial `WHERE` are supported. No `ASC`/`DESC`. Max 8 key columns, 24 indexes per table. Returns a `job_id`; failed builds leave an `INVALID` index that must be dropped. Track with `sys.jobs` / `sys.wait_for_job(job_id)`.
- **Max 1 DDL per transaction**, and DDL and DML can't be mixed. Each DDL statement runs in its own implicit transaction.
- **ALTER TABLE** supports: `ADD COLUMN`, `DROP COLUMN` (not PK columns), `SET/DROP DEFAULT`, `DROP NOT NULL`, `DROP EXPRESSION`, `ADD GENERATED ... AS IDENTITY` (bigint, explicit `CACHE`), `SET GENERATED`, `DROP IDENTITY`, `ADD CONSTRAINT {CHECK|FOREIGN KEY} ... NOT VALID` (NOT VALID is mandatory), `ADD CONSTRAINT ... UNIQUE USING INDEX` (index must be VALID), `DROP CONSTRAINT`, `ALTER TABLE ASYNC ... VALIDATE CONSTRAINT` (async job), `RENAME`, `SET SCHEMA`, `OWNER TO`.
- **ALTER TABLE does not support**: `ALTER COLUMN TYPE`, `SET NOT NULL`, `ADD PRIMARY KEY`, `ADD UNIQUE` without `USING INDEX`.
- **Foreign keys** (since 2026-08): inline `REFERENCES` or table-level `FOREIGN KEY`, all referential actions, `MATCH SIMPLE|FULL`, `DEFERRABLE` (FKs only). Cascades count toward the 3,000-row transaction limit.
- **Sequences and identity** (since 2026-02): `bigint` only; `CACHE` must be explicit and be `1` or `>= 65536`. `SERIAL` types are not documented and are rejected by `verify`. Identity `CACHE` is only documented as required for `ADD GENERATED`; `verify` also requires it in `CREATE TABLE` (inferred from CREATE SEQUENCE, since identity options are sequence options).
- **Extended statistics** (since 2026-08): `CREATE/ALTER/DROP STATISTICS`, max 5 per table.
- **ALTER SYSTEM** (since 2026-11): `SET`/`RESET`/`RESET ALL` for a managed subset of parameters (e.g. `dsql.enable_batched_nestloop`); must run outside an explicit transaction.
- **Data types**: adds `json`/`jsonb` (not indexable, compressed) and `numeric` up to precision 1000 (unconstrained `numeric` is stored as `numeric(18,6)`). Arrays and `inet` are runtime-only, not storable.
- **No `SET` for timeout session parameters**: `statement_timeout`, `lock_timeout` etc. are blocked by the proxy (not documented either way). `SET dsql.enable_batched_nestloop`, `TimeZone`, `search_path` are fine.
- **IAM authentication**: Passwords are IAM-signed presigned URL tokens.
- **TLS required**: All connections use `sslmode=require`.
- **Endpoint format**: `<cluster-id>.dsql.<region>.on.aws`, port 5432.
- **No extensions, custom/composite types, enums, triggers, procedures, temp tables, or row-level security**.
- **No TRUNCATE** (use `DELETE FROM`).
- **No table partitioning or inheritance** (DSQL auto-partitions).
- **Functions**: only `LANGUAGE SQL` supported.
- **Collation**: `C` only.

## Supported DDL Operations

The diff engine generates these DDL statements:

| Operation | Supported | Notes |
|-----------|-----------|-------|
| CREATE TABLE | Yes | Topologically ordered by FKs; FK cycles are added afterwards with `ADD ... NOT VALID` |
| DROP TABLE | Yes | Hazard: `DELETES_DATA`; referencing tables dropped first |
| ADD COLUMN | Yes | |
| DROP COLUMN | Yes | Hazard: `DELETES_DATA`; error for PK columns |
| ALTER COLUMN TYPE | No | Error at plan time |
| DROP NOT NULL | Yes | |
| SET NOT NULL | No | Error at plan time |
| SET/DROP DEFAULT | Yes | |
| ADD/SET/DROP identity | Yes | ADD requires bigint + explicit `CACHE`; DROP IDENTITY hazard: `CORRECTNESS` |
| DROP EXPRESSION | Yes | Adding/changing a generation expression is an error |
| PRIMARY KEY change | No | Error at plan time |
| ADD CHECK / FOREIGN KEY | Yes | `ADD CONSTRAINT ... NOT VALID` + `ALTER TABLE ASYNC ... VALIDATE CONSTRAINT` (hazard: `CONSTRAINT_VALIDATION`) |
| ADD UNIQUE | Yes | `CREATE UNIQUE INDEX ASYNC` (runner waits via `sys.wait_for_job`) + `ADD CONSTRAINT ... UNIQUE USING INDEX` |
| DROP CONSTRAINT | Yes | Hazard: `CORRECTNESS` |
| VALIDATE CONSTRAINT | Yes | Emitted for live constraints still `NOT VALID` |
| CREATE INDEX ASYNC | Yes | Hazard: `INDEX_BUILD`; expressions, partial, INCLUDE, NULLS NOT DISTINCT |
| DROP INDEX | Yes | Hazard: `INDEX_DROPPED`; definition change = drop + create |
| CREATE/ALTER/DROP SEQUENCE | Yes | Standalone sequences only (identity sequences excluded); DROP hazard: `DELETES_DATA` |
| CREATE/DROP STATISTICS | Yes | Definition change = drop + create |

## Connection Details

- Use `pgx/v4/stdlib.OpenDB` to get a `*sql.DB`.
- Set `MaxOpenConns(1)` to avoid token-expiry issues on pooled connections.
- Region auto-detected from endpoint hostname (`*.dsql.<region>.on.aws`).
- IAM tokens default to 15-minute expiry; connections remain valid after token expires.
- DNS resolved to all IPs; pgx fallback configs try each IP on connection failure.
- AWS credentials resolved via default chain (env vars → shared config → IMDS); optional `--profile` and `--role-arn` for override/assumption.

## CLI Flags

### Global flags

| Flag | Description | Default |
|------|-------------|---------|
| `--endpoint` | Aurora DSQL cluster endpoint | (required) |
| `--region` | AWS region | auto-detected from endpoint |
| `--user` | Database user | `admin` |
| `--schema` | `.sql` files or directories (repeatable) | (required) |
| `--profile` | AWS profile name | `$AWS_PROFILE` |
| `--role-arn` | AWS IAM role ARN to assume via STS | (none) |
| `--connect-timeout` | Database connection timeout | `10s` |

### `apply` subcommand flags

| Flag | Description | Default |
|------|-------------|---------|
| `--allow-hazards` | Hazard types to permit (e.g. `INDEX_BUILD,DELETES_DATA`) | (none) |
| `--force` | Apply without confirmation prompt | `false` |
| `--retries` | Max retries on OCC conflict (SQLSTATE 40001) | `5` |
| `--retry-delay` | Initial delay between retries (doubles each attempt) | `2s` |

### `proxy` subcommand flags

| Flag | Description | Default |
|------|-------------|---------|
| `--listen` | Address to listen on | `:15432` |
| `--upstream` | Backend PostgreSQL address | `localhost:5432` |

### `sql` subcommand flags

| Flag | Description | Default |
|------|-------------|---------|
| `--retries` | Max retries on OCC conflict (SQLSTATE 40001) | `5` |
| `--retry-delay` | Initial delay between retries (doubles each attempt) | `2s` |

## Migration Design

- **Stateless**: No migration history table. Plans are idempotent — always diffs current live schema vs desired `.sql` files.
- **No temp database**: SQL files are parsed in-process (regex/tokenizer-based), not executed against any database.
- **Custom schema diffing**: Parses `.sql` files into models, introspects live schema via `pg_catalog`, and diffs in-process.
- **Phased DDL ordering** (`internal/schema/diff.go`): DROP INDEX → DROP CONSTRAINT → DROP STATISTICS → DROP COLUMN → DROP TABLE → CREATE SEQUENCE → ALTER SEQUENCE → CREATE TABLE → ALTER COLUMN (add/default/nullability/identity/expression, OWNED BY) → CREATE INDEX ASYNC → ADD CONSTRAINT (NOT VALID / USING INDEX) → VALIDATE CONSTRAINT → CREATE STATISTICS → DROP SEQUENCE.
- **Unsupported operations** (error at plan time): ALTER COLUMN TYPE, SET NOT NULL, PRIMARY KEY changes, dropping PK columns, adding/changing generation expressions, ADD identity on non-bigint or without CACHE.
- **Constraint matching**: unique by columns + NULLS NOT DISTINCT, check by normalized expression, FK by full definition (columns, ref table/columns, actions, match, deferrability) — names are not compared.
- **Async jobs**: `Statement.WaitForJob` makes the runner read the returned `job_id` and block on `SELECT sys.wait_for_job($1)`. If no job_id row comes back (PostgreSQL behind the proxy), it continues.
- **OCC retry**: Statements that fail with SQLSTATE 40001 are retried with exponential backoff.
- **No transactions**: Each statement executes independently; DSQL limits 1 DDL per transaction.

## Change Actions (Terraform-style)

| Symbol | Action | Description |
|--------|--------|-------------|
| `+` | Create | A new resource will be provisioned |
| `~` | Update | An existing resource will be modified in place |
| `-` | Destroy | An existing resource will be deleted |
| `+/-` | Replace | An existing resource will be destroyed and recreated |

## Hazard Types

| Type | Description |
|------|-------------|
| `DELETES_DATA` | Statement drops a table, column, or sequence and its data |
| `INDEX_BUILD` | Async index build in progress |
| `INDEX_DROPPED` | Dropping an index may degrade query performance |
| `CORRECTNESS` | Drops a constraint or identity, loosening data-integrity guarantees |
| `CONSTRAINT_VALIDATION` | Async VALIDATE CONSTRAINT on an existing table; job fails if rows violate it |

## Schema Parsing

- `.sql` files are parsed with regex and a paren-aware tokenizer, not executed against a database. Statements are split by `rules.Split` (handles quotes, `$$` bodies, comments).
- Parsed into models: `CREATE TABLE` (columns, PK, UNIQUE [NULLS NOT DISTINCT], CHECK, FOREIGN KEY inline/table-level, identity with options, `GENERATED ALWAYS AS (...) STORED`), `CREATE [UNIQUE] INDEX ASYNC` (expressions, INCLUDE, NULLS NOT DISTINCT, WHERE), `CREATE SEQUENCE`, `CREATE STATISTICS`. Index/statistics may appear before their table.
- `CREATE VIEW`, `CREATE FUNCTION`, `CREATE DOMAIN/ROLE/SCHEMA`, `ALTER SYSTEM`, `ALTER DEFAULT PRIVILEGES`, `ALTER ROUTINE`, `COMMENT ON`, `GRANT`/`REVOKE`, etc. are recognized and skipped (not managed by the diff engine).
- Unnamed constraints get PostgreSQL-style names (`<table>_<cols>_key|_check|_fkey`, `_pkey`), de-duplicated with numeric suffixes. PK and identity columns are implicitly NOT NULL. FK ref columns default to the referenced table's PK.
- Type normalization maps aliases to canonical forms (e.g., `int` → `integer`, `bool` → `boolean`, `decimal` → `numeric`); `numeric` equals `numeric(18,6)` when diffing.
- Default expressions normalized: strips redundant parens and trivial type casts (`'open'::text` → `'open'`, `'s'::regclass` → `'s'`).
- Check, index-key, partial-predicate, generated, and statistics expressions are normalized with `normalizeCheck` (casts and parens stripped, `= ANY (ARRAY[...])` → `IN (...)`). Index keys drop default `ASC` / `NULLS LAST`.
- Introspection queries `pg_class`, `pg_attribute` (incl. `attgenerated`), `pg_constraint` (`pg_get_constraintdef`, `NOT VALID` suffix → `NotValid`), `pg_index` (`pg_get_indexdef`, parsed with the same index parser; DSQL prints `USING btree_index`), `pg_statistic_ext` (`pg_get_statisticsobjdef`), and `pg_sequence` (excluding identity-owned sequences) in the `public` schema.

## Proxy Design

- **TCP-level proxy**: Sits between a PostgreSQL client and backend, speaking the PG wire protocol via `pgproto3/v2`.
- **SQL interception**: Inspects `Query` (simple protocol) and `Parse` (extended protocol) messages; blocks statements matching DSQL-unsupported patterns.
- **Shared rules**: DSQL compatibility rules are defined in `internal/rules/rules.go` and shared between `verify` and `proxy`. `SharedRules` are line-safe; `StatementRules` need the whole statement (multi-line ALTER TABLE / sequence / identity / index checks) — `verify` runs them per statement and reports the statement's first line. `Rule.Unless` expresses "X without Y" (no lookahead in Go regexp); `Rule.Func` allows custom checks. Each consumer adds context-specific rules on top.
- **Proxy-specific rules**: Additional checks for `CREATE TABLE AS`, `CREATE TYPE ENUM/RANGE`, `VACUUM`, `COLLATE`, `SET` timeout session params, non-REPEATABLE-READ isolation, `SAVEPOINT`, `LISTEN`/`NOTIFY`, `LOCK TABLE`. `ALTER SYSTEM`, foreign keys, and JSON/JSONB are allowed.
- **SQL rewriting**: `CREATE INDEX ASYNC` → `CREATE INDEX CONCURRENTLY` and `ALTER TABLE ASYNC` → `ALTER TABLE` for the PostgreSQL backend.
- **Transaction warnings**: Logs warnings for multiple DDL or mixed DDL+DML in one transaction (DSQL limits).
- **Auth bypass**: When `POSTGRES_USER`/`POSTGRES_PASSWORD`/`POSTGRES_DB` env vars are set, accepts any client auth and authenticates with the backend using those credentials (supports trust, MD5, SCRAM-SHA-256).
- **Error handling**: Blocked statements return a PostgreSQL `ErrorResponse` (SQLSTATE `0A000`) followed by `ReadyForQuery`, keeping the client connection alive.
- **Logging**: Incoming SQL (`-->`), deesql blocks (`[deesql]`), PostgreSQL errors (`[postgres]`) with DSQL-specific hints.
- **SSL handling**: Responds `N` to `SSLRequest` and proceeds with plaintext (proxy is for local development use).
- **CancelRequest**: Forwarded directly to the backend.
- **Statement splitting**: Multi-statement strings (separated by `;`) are split and each checked independently, respecting single-quoted string literals.

## Build & Run

```sh
go build -o deesql .
./deesql plan --endpoint <endpoint> --schema ./schema
./deesql apply --endpoint <endpoint> --schema ./schema
./deesql apply --endpoint <endpoint> --schema ./schema --force
./deesql verify --schema ./schema
./deesql proxy --listen :15432 --upstream localhost:5432
./deesql sql cleanup.sql --endpoint <endpoint>
```

## Testing

```sh
make test                    # Unit tests with race detection and coverage

# E2E tests (from tests/docker-compose/):
make test-docker             # Through deesql proxy + PostgreSQL
make test-psql               # Bare PostgreSQL (baseline)
make test-dsql DSQL=<endpoint>  # Real Aurora DSQL (also runs in CI)

# E2E plan/apply through the proxy (from tests/tools/):
make test                    # verify → plan → apply → no-drift, then migrate schema/ → schema_v2/ (all ALTER paths) → no-drift
```

## Go Code Styles

When function takes more than 2 arguments (parameters), use struct input for future scalability.
Use [validator](https://github.com/go-playground/validator) to validate parameters.

```go
// Define input as struct, and add `validate` tags.
type CalculateInput struct {
    Amount int   `validate:"required"`
    Tax    float `validate:"required"`
}

// Define output as struct.
type CalculateOutput struct {
    Value int
}

// 1. Always set ctx at the first parameter, input as second parameter
// 2. Always return pointer output and error as output
func Calculate(ctx context.Context, in CalculateInput) (*CalculateOutput, error) (

  // Make sure to validate!
  if err := validator.Validate(in); err != nil {
      return nil, errors.Wrap(validator.ValidationError, err.Error())
  }

  ...

  // Cast output
  return &CalculateOutput{
      Value: calculated,
  }, nil
}
```

For tests, have 1 Test prefixed function which corresponds to the implementation.
Do not write 2+ functions for single implementation, instead separate them using t.Run.
Use [testify](https://github.com/stretchr/testify) to avoid redundant code.

```go
func TestSomething(t *testing.T) {
    t.Run("success case", func(t *testing.T) {}
      ...
    }

    t.Run("fail case, mocked", func(t *testing.T) {
      ...
    }

    // You can have as much patterns you want
    t.Run("fail case, calling real database", func(t *testing.T) {
      ...
    }
}

// NEVER branch your test using wantErr
func TestSomething(t *testing.T) {
    patterns := []struct{
       input   *SomeInput
       wantErr bool // NEVER DO THIS
    }

    for _, p := range patterns {
        // Branching causes if-else conditionals inside test
        // and we don't like that.
        if p.wantErr {
            // error case
            continue
        } else {
            // success case
        }
    }
}
```
