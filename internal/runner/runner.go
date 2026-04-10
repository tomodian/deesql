package runner

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"tomodian/deesql/internal/schema"
	"tomodian/deesql/internal/ui"

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
	DB             *sql.DB       `validate:"required"`
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

		if err := executeWithRetry(ctx, in.DB, stmt, i+1, in.MaxRetries, in.RetryBaseDelay); err != nil {
			return err
		}
		ui.Success("Statement %d completed", i+1)
	}
	return nil
}

func executeWithRetry(ctx context.Context, db *sql.DB, stmt schema.Statement, num int, maxRetries int, baseDelay time.Duration) error {
	delay := baseDelay

	for attempt := 0; attempt <= maxRetries; attempt++ {
		_, err := db.ExecContext(ctx, stmt.ToSQL())
		if err == nil {
			return nil
		}

		// Retry on OCC conflict (SQLSTATE 40001).
		if isOCCError(err) && attempt < maxRetries {
			ui.Warn("Statement %d: OCC conflict, retrying in %s (%d/%d)...", num, delay, attempt+1, maxRetries)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			delay *= 2
			continue
		}

		ui.Error("Statement %d failed: %s", num, err)
		return fmt.Errorf("statement %d failed (%s): %w", num, stmt.DDL, err)
	}
	return nil
}

// isOCCError checks if the error is a DSQL optimistic concurrency conflict (SQLSTATE 40001).
func isOCCError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "40001")
}
