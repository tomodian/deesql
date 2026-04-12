package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"tomodian/deesql/internal/planner"
	"tomodian/deesql/internal/runner"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

// GeneratePlanInput is the input for GeneratePlan.
type GeneratePlanInput struct {
	DB          *sql.DB  `validate:"required"`
	SchemaFiles []string `validate:"required,min=1"`
}

// GeneratePlanOutput is the output for GeneratePlan.
type GeneratePlanOutput struct {
	Plan Plan
}

// GeneratePlan parses desired schema from SQL files, introspects the live
// database schema, and produces a migration plan.
func GeneratePlan(ctx context.Context, in GeneratePlanInput) (*GeneratePlanOutput, error) {
	if err := validate.Struct(in); err != nil {
		return nil, fmt.Errorf("invalid plan input: %w", err)
	}

	if out, err := planner.GeneratePlan(ctx, planner.GeneratePlanInput{
		DB:          in.DB,
		SchemaFiles: in.SchemaFiles,
	}); err != nil {
		return nil, fmt.Errorf("generating plan: %w", err)
	} else {
		return &GeneratePlanOutput{
			Plan: fromInternal(out.Plan),
		}, nil
	}
}

// CheckHazardsInput is the input for CheckHazards.
type CheckHazardsInput struct {
	Plan           Plan
	AllowedHazards []string
}

// CheckHazards validates the plan against allowed hazard types.
// Returns an error if any unallowed hazards are present.
func CheckHazards(ctx context.Context, in CheckHazardsInput) error {
	return runner.CheckHazards(ctx, runner.CheckHazardsInput{
		Plan:           toInternal(in.Plan),
		AllowedHazards: in.AllowedHazards,
	})
}

// ApplyInput is the input for Apply.
type ApplyInput struct {
	DB             *sql.DB       `validate:"required"`
	Plan           Plan
	MaxRetries     int
	RetryBaseDelay time.Duration
}

// Apply executes the migration plan against the database.
// Statements that fail with OCC conflicts (SQLSTATE 40001) are retried
// with exponential backoff.
func Apply(ctx context.Context, in ApplyInput) error {
	if err := validate.Struct(in); err != nil {
		return fmt.Errorf("invalid apply input: %w", err)
	}

	if len(in.Plan.Statements) == 0 {
		return nil
	}

	return runner.Execute(ctx, runner.ExecuteInput{
		DB:             in.DB,
		Plan:           toInternal(in.Plan),
		MaxRetries:     in.MaxRetries,
		RetryBaseDelay: in.RetryBaseDelay,
	})
}

// PlanSummary returns a text summary of the plan (e.g. "2 to create, 1 to destroy").
func PlanSummary(p Plan) string {
	counts := make(map[Action]int)
	for _, s := range p.Statements {
		counts[s.Action]++
	}

	var parts []string
	if n := counts[ActionCreate]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d to create", n))
	}
	if n := counts[ActionUpdate]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d to update", n))
	}
	if n := counts[ActionDestroy]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d to destroy", n))
	}
	if n := counts[ActionReplace]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d to replace", n))
	}

	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, ", ")
}
