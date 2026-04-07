package runner

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"tomodian/dsql-migrate/internal/schema"
	"tomodian/dsql-migrate/internal/ui"

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
	DB   *sql.DB     `validate:"required"`
	Plan schema.Plan
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
func Execute(ctx context.Context, in ExecuteInput) error {
	if err := validate.Struct(in); err != nil {
		return fmt.Errorf("invalid execute input: %w", err)
	}

	total := len(in.Plan.Statements)
	for i, stmt := range in.Plan.Statements {
		ui.Step("(%d/%d) %s", i+1, total, stmt.DDL)

		if _, err := in.DB.ExecContext(ctx, stmt.ToSQL()); err != nil {
			ui.Error("Statement %d failed: %s", i+1, err)
			return fmt.Errorf("statement %d failed (%s): %w", i+1, stmt.DDL, err)
		}
		ui.Success("Statement %d completed", i+1)
	}
	return nil
}
