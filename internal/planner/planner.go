package planner

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/tomodian/deesql/internal/schema"
	"github.com/tomodian/deesql/internal/ui"

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
	Plan schema.Plan
}

// GeneratePlan parses desired schema from SQL files, introspects the live
// DSQL schema, and produces a migration plan.
func GeneratePlan(ctx context.Context, in GeneratePlanInput) (*GeneratePlanOutput, error) {
	if err := validate.Struct(in); err != nil {
		return nil, fmt.Errorf("invalid plan input: %w", err)
	}

	ui.Step("Reading desired schema from: %s", strings.Join(in.SchemaFiles, ", "))

	parseOut, err := schema.Parse(ctx, schema.ParseInput{Files: in.SchemaFiles})
	if err != nil {
		return nil, fmt.Errorf("parsing schema files: %w", err)
	}

	introOut, err := schema.Introspect(ctx, schema.IntrospectInput{DB: in.DB})
	if err != nil {
		return nil, fmt.Errorf("introspecting live schema: %w", err)
	}

	diffOut, err := schema.Diff(ctx, schema.DiffInput{
		Current: introOut.Schema,
		Desired: parseOut.Schema,
	})
	if err != nil {
		return nil, fmt.Errorf("computing diff: %w", err)
	}

	return &GeneratePlanOutput{Plan: diffOut.Plan}, nil
}
