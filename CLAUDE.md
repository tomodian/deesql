# deesql

CLI tool for schema migrations on Amazon Aurora DSQL.

Go module: `tomodian/deesql`

## Commands

- `plan` — Connect to DSQL, diff live schema against desired-state `.sql` files, print migration plan.
- `apply` — Same as plan, then execute the migration statements.
- `verify` — Check `.sql` files for DSQL compatibility without connecting to a database.
- `proxy` — Start a DSQL-filtering TCP proxy between a PostgreSQL client and backend that intercepts and blocks unsupported SQL statements.

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
    rules/rules.go              # Shared DSQL compatibility Rule type and SharedRules
    verify/verify.go            # Regex-based DSQL compatibility checker (static .sql files)
    proxy/
      server.go                 # TCP listener and graceful shutdown
      handler.go                # Per-connection PG protocol relay (startup, auth, steady-state)
      intercept.go              # SQL interception: Check(), splitStatements(), proxy-specific rules
    ui/ui.go                    # Colored terminal output helpers
  tests/
    simple/                     # Example desired-state schema files
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

Official reference: [Aurora DSQL supported SQL subsets](https://docs.aws.amazon.com/aurora-dsql/latest/userguide/working-with-postgresql-compatibility-supported-sql-subsets.html) | [Unsupported features](https://docs.aws.amazon.com/aurora-dsql/latest/userguide/working-with-postgresql-compatibility-unsupported-features.html) | [ALTER TABLE syntax](https://docs.aws.amazon.com/aurora-dsql/latest/userguide/alter-table-syntax-support.html)

- **No `CREATE DATABASE`**: Only the `postgres` database exists per cluster.
- **No `CREATE INDEX`**: Use `CREATE INDEX ASYNC` instead (only btree).
- **No `SET` for session parameters**: `statement_timeout`, `lock_timeout` etc. are not supported.
- **Max 1 DDL per transaction**: Each DDL statement runs in its own implicit transaction.
- **IAM authentication**: Passwords are IAM-signed presigned URL tokens.
- **TLS required**: All connections use `sslmode=require`.
- **Endpoint format**: `<cluster-id>.dsql.<region>.on.aws`, port 5432.
- **No extensions, custom types, triggers, or sequences** (use `GENERATED ... AS IDENTITY`).
- **No FOREIGN KEY constraints**.
- **No TRUNCATE** (use `DELETE FROM`).
- **No table partitioning or inheritance** (DSQL auto-partitions).
- **Functions**: only `LANGUAGE SQL` supported.

## Supported DDL Operations

The diff engine generates these DDL statements:

| Operation | Supported | Notes |
|-----------|-----------|-------|
| CREATE TABLE | Yes | |
| DROP TABLE | Yes | Hazard: `DELETES_DATA` |
| ADD COLUMN | Yes | |
| DROP COLUMN | No | Error at plan time |
| ALTER COLUMN TYPE | No | Error at plan time |
| SET/DROP NOT NULL | No | Error at plan time |
| SET/DROP DEFAULT | No | Error at plan time |
| PRIMARY KEY change | No | Error at plan time |
| ADD/DROP CONSTRAINT | Yes | UNIQUE and CHECK only |
| CREATE INDEX ASYNC | Yes | Hazard: `INDEX_BUILD` |
| DROP INDEX | Yes | Hazard: `INDEX_DROPPED` |

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

### `proxy` subcommand flags

| Flag | Description | Default |
|------|-------------|---------|
| `--listen` | Address to listen on | `:15432` |
| `--upstream` | Backend PostgreSQL address | `localhost:5432` |

## Migration Design

- **Stateless**: No migration history table. Plans are idempotent — always diffs current live schema vs desired `.sql` files.
- **No temp database**: SQL files are parsed in-process (regex-based), not executed against any database.
- **Custom schema diffing**: Parses `.sql` files into models, introspects live schema via `pg_catalog`, and diffs in-process.
- **7-phase DDL ordering**: DROP INDEX → DROP CONSTRAINT → DROP TABLE → CREATE TABLE → ADD COLUMN → ADD CONSTRAINT → CREATE INDEX ASYNC.
- **Unsupported operations** (error at plan time): DROP COLUMN, ALTER COLUMN TYPE, SET/DROP NOT NULL, SET/DROP DEFAULT, PRIMARY KEY changes.
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
| `DELETES_DATA` | Statement drops a table and its data |
| `INDEX_BUILD` | Async index build in progress |
| `INDEX_DROPPED` | Dropping an index may degrade query performance |
| `CORRECTNESS` | Statement may affect data correctness |

## Schema Parsing

- `.sql` files are parsed with regex, not executed against a database.
- Only `CREATE TABLE` and `CREATE [UNIQUE] INDEX ASYNC` statements are supported.
- Type normalization maps aliases to canonical forms (e.g., `int` → `integer`, `bool` → `boolean`).
- Default expressions normalized: strips redundant parens and trivial type casts (`'open'::text` → `'open'`).
- Check expressions normalized: `= ANY (ARRAY[...])` → `IN (...)`.
- Introspection queries `pg_class`, `pg_attribute`, `pg_constraint`, and `pg_index` in the `public` schema.

## Proxy Design

- **TCP-level proxy**: Sits between a PostgreSQL client and backend, speaking the PG wire protocol via `pgproto3/v2`.
- **SQL interception**: Inspects `Query` (simple protocol) and `Parse` (extended protocol) messages; blocks statements matching DSQL-unsupported patterns.
- **Shared rules**: DSQL compatibility rules are defined in `internal/rules/rules.go` and shared between `verify` and `proxy`. Each consumer adds context-specific rules on top.
- **Proxy-specific rules**: Additional checks for `CREATE TABLE AS`, `CREATE TYPE ENUM/RANGE`, `ALTER SYSTEM`, `VACUUM`, `COLLATE`, and index ordering (`ASC`/`DESC`).
- **Error handling**: Blocked statements return a PostgreSQL `ErrorResponse` (SQLSTATE `0A000`) followed by `ReadyForQuery`, keeping the client connection alive.
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
```

## Testing

```sh
make test          # Run all tests with race detection and coverage
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
