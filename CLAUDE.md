# dsql-migrate

CLI tool for schema migrations on Amazon Aurora DSQL.

Go module: `tomodian/dsql-migrate`

## Commands

- `plan` — Connect to DSQL, diff live schema against desired-state `.sql` files, print migration plan.
- `apply` — Same as plan, then execute the migration statements.
- `verify` — Check `.sql` files for DSQL compatibility without connecting to a database.

## Architecture

```
migrate/
  main.go                       # Entry point
  cli/
    root.go                     # Root urfave/cli v3 app + shared flags
    plan.go                     # "plan" subcommand
    apply.go                    # "apply" subcommand
    verify.go                   # "verify" subcommand
    helpers.go                  # Flag extraction and validation helpers
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
    verify/verify.go            # Regex-based DSQL compatibility checker
  tests/
    simple/                     # Example desired-state schema files
```

## Dependencies

- `github.com/urfave/cli/v3` — CLI framework
- `github.com/jackc/pgx/v4` — PostgreSQL driver
- `github.com/jackc/pgconn` — low-level PG connection (fallback configs)
- `github.com/go-playground/validator/v10` — struct validation
- `github.com/aws/aws-sdk-go-v2`, `config`, `credentials/stscreds`, `feature/dsql/auth`, `service/sts` — IAM auth and role assumption

## Aurora DSQL Constraints

- **No `CREATE DATABASE`**: Only the `postgres` database exists per cluster.
- **No `CREATE INDEX`**: Use `CREATE INDEX ASYNC` instead (only btree).
- **No `SET` for configuration parameters**: `statement_timeout`, `lock_timeout` etc. are not supported.
- **Max 1 DDL per transaction**: Each DDL statement runs in its own implicit transaction.
- **IAM authentication**: Passwords are IAM-signed presigned URL tokens.
- **TLS required**: All connections use `sslmode=require`.
- **Endpoint format**: `<cluster-id>.dsql.<region>.on.aws`, port 5432.
- **No extensions, custom types, triggers, or sequences** (use `GENERATED ... AS IDENTITY`).
- **No FOREIGN KEY constraints**.
- **No TRUNCATE** (use `DELETE FROM`).
- **No table partitioning or inheritance** (DSQL auto-partitions).
- **Functions**: only `LANGUAGE SQL` supported.

## ALTER TABLE Support

| Operation | Supported |
|-----------|-----------|
| ADD COLUMN | Yes |
| DROP COLUMN | Yes (except shard key / PK columns) |
| RENAME COLUMN | Yes |
| RENAME TABLE | Yes |
| Identity modifications | Yes |
| ALTER COLUMN TYPE | No |
| SET/DROP NOT NULL | No |
| SET/DROP DEFAULT | No |

## Connection Details

- Use `pgx/v4/stdlib.OpenDB` to get a `*sql.DB`.
- Set `MaxOpenConns(1)` to avoid token-expiry issues on pooled connections.
- Region auto-detected from endpoint hostname (`*.dsql.<region>.on.aws`).
- IAM tokens default to 15-minute expiry; connections remain valid after token expires.
- DNS resolved to all IPs; pgx fallback configs try each IP on connection failure.
- AWS credentials resolved via default chain: env vars → shared config → IMDS. Optional `--profile` and `--role-arn`.

## CLI Flags

### Global flags

| Flag | Description | Default |
|------|-------------|---------|
| `--endpoint` | Aurora DSQL cluster endpoint | (required) |
| `--region` | AWS region | auto-detected from endpoint |
| `--user` | Database user | `admin` |
| `--schema` | Directories with desired-state `.sql` files (repeatable) | (required) |
| `--profile` | AWS profile name | `$AWS_PROFILE` |
| `--role-arn` | AWS IAM role ARN to assume via STS | (none) |
| `--connect-timeout` | Database connection timeout | `10s` |

### `apply` subcommand flags

| Flag | Description | Default |
|------|-------------|---------|
| `--allow-hazards` | Hazard types to permit (e.g. `INDEX_BUILD,DELETES_DATA`) | (none) |
| `--skip-confirm` | Skip confirmation prompt | `false` |

## Migration Design

- **Stateless**: No migration history table. Plans are idempotent — always diffs current live schema vs desired `.sql` files.
- **No temp database**: SQL files are parsed in-process (regex-based), not executed against any database.
- **Custom schema diffing**: Parses `.sql` files into models, introspects live schema via `pg_catalog`, and diffs in-process.
- **7-phase DDL ordering**: DROP INDEX → DROP CONSTRAINT → DROP TABLE → CREATE TABLE → ADD COLUMN → ADD CONSTRAINT → CREATE INDEX ASYNC.
- **Unsupported ALTER operations**: DROP COLUMN, ALTER COLUMN TYPE, SET/DROP NOT NULL, SET/DROP DEFAULT, and PRIMARY KEY changes are errors (Aurora DSQL limitations).
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

## Build & Run

```sh
go build -o dsql-migrate .
./dsql-migrate plan --endpoint <endpoint> --schema ./schema
./dsql-migrate apply --endpoint <endpoint> --schema ./schema
./dsql-migrate verify --schema ./schema
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
