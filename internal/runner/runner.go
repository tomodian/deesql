package runner

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/tomodian/deesql/internal/schema"
	"github.com/tomodian/deesql/internal/ui"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

// CheckHazardsInput is the input for CheckHazards.
type CheckHazardsInput struct {
	Plan           schema.Plan
	AllowedHazards []string
}

// ExecuteInput is the input for Execute.
type ExecuteInput struct {
	DB             *sql.DB `validate:"required"`
	Plan           schema.Plan
	MaxRetries     int
	RetryBaseDelay time.Duration
}

// CheckHazards validates the plan against allowed hazard types.
func CheckHazards(ctx context.Context, in CheckHazardsInput) error {
	allowSet := make(map[string]bool, len(in.AllowedHazards))
	for _, h := range in.AllowedHazards {
		allowSet[strings.TrimSpace(h)] = true
	}

	var blocked []string
	for _, stmt := range in.Plan.Statements {
		for _, h := range stmt.Hazards {
			if !allowSet[h.Type] {
				blocked = append(blocked, fmt.Sprintf("  %s: %s (statement: %s)", h.Type, h.Message, stmt.DDL))
			}
		}
	}

	if len(blocked) > 0 {
		return fmt.Errorf("migration blocked by hazards (use --allow-hazards to permit):\n%s", strings.Join(blocked, "\n"))
	}
	return nil
}

// Execute applies the migration plan to the database.
// Statements that fail with OCC conflicts (SQLSTATE 40001) are retried
// with exponential backoff, since DSQL uses optimistic concurrency control
// and concurrent DDL (e.g., async index builds) can cause transient conflicts.
func Execute(ctx context.Context, in ExecuteInput) error {
	if err := validate.Struct(in); err != nil {
		return fmt.Errorf("invalid execute input: %w", err)
	}

	total := len(in.Plan.Statements)
	for i, stmt := range in.Plan.Statements {
		ui.Step("(%d/%d) %s", i+1, total, stmt.DDL)

		if err := executeWithRetry(ctx, executeWithRetryInput{
			DB:         in.DB,
			SQL:        stmt.ToSQL(),
			DDL:        stmt.DDL,
			Num:        i + 1,
			MaxRetries: in.MaxRetries,
			BaseDelay:  in.RetryBaseDelay,
			WaitForJob: stmt.WaitForJob,
		}); err != nil {
			return err
		}
		ui.Success("Statement %d completed", i+1)
	}
	return nil
}

type executeWithRetryInput struct {
	DB         *sql.DB `validate:"required"`
	SQL        string  `validate:"required"`
	DDL        string
	Num        int
	MaxRetries int
	BaseDelay  time.Duration
	WaitForJob bool
}

func executeWithRetry(ctx context.Context, in executeWithRetryInput) error {
	delay := in.BaseDelay

	for attempt := 0; attempt <= in.MaxRetries; attempt++ {
		err := execStatement(ctx, in.DB, in.SQL, in.WaitForJob, in.Num)
		if err == nil {
			return nil
		}

		// Retry on OCC conflict (SQLSTATE 40001).
		if isOCCError(err) && attempt < in.MaxRetries {
			ui.Warn("Statement %d: OCC conflict, retrying in %s (%d/%d)...", in.Num, delay, attempt+1, in.MaxRetries)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			delay *= 2
			continue
		}

		ui.Error("Statement %d failed: %s", in.Num, err)
		return fmt.Errorf("statement %d failed (%s): %w", in.Num, in.DDL, err)
	}
	return nil
}

// execStatement runs a single statement. When waitForJob is set, the
// statement is expected to return an async job_id (Aurora DSQL's CREATE INDEX
// ASYNC / ALTER TABLE ASYNC); the call then blocks on sys.wait_for_job until
// the job finishes. If no job_id is returned (e.g. PostgreSQL behind the
// deesql proxy, which builds the index synchronously) there is nothing to wait for.
func execStatement(ctx context.Context, db *sql.DB, query string, waitForJob bool, num int) error {
	if !waitForJob {
		_, err := db.ExecContext(ctx, query)
		return err
	}

	jobID, err := queryJobID(ctx, db, query)
	if err != nil || jobID == "" {
		return err
	}

	ui.Dim("    Waiting for async job %s...\n", jobID)
	var ok bool
	if err := db.QueryRowContext(ctx, "SELECT sys.wait_for_job($1)", jobID).Scan(&ok); err != nil {
		return fmt.Errorf("waiting for job %s: %w", jobID, err)
	}
	if !ok {
		return fmt.Errorf("statement %d: async job %s did not complete successfully (check sys.jobs)", num, jobID)
	}
	return nil
}

func queryJobID(ctx context.Context, db *sql.DB, query string) (string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var jobID string
	if rows.Next() {
		if err := rows.Scan(&jobID); err != nil {
			return "", fmt.Errorf("reading job_id: %w", err)
		}
	}
	return jobID, rows.Err()
}

// isOCCError checks if the error is a DSQL optimistic concurrency conflict (SQLSTATE 40001).
func isOCCError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "40001")
}
