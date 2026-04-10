package cli

import (
	"context"
	"fmt"

	"tomodian/deesql/internal/dsqlconn"
	"tomodian/deesql/internal/output"
	"tomodian/deesql/internal/planner"

	"github.com/urfave/cli/v3"
)

func planCmd() *cli.Command {
	return &cli.Command{
		Name:  "plan",
		Usage: "Generate and display a migration plan",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if err := requireFlags(cmd); err != nil {
				return err
			}

			files, err := resolveSchemaFiles(cmd.StringSlice(FlagSchema))
			if err != nil {
				return err
			}

			if err := verifySchemaFiles(files); err != nil {
				return err
			}

			out, err := dsqlconn.Connect(ctx, connectInputFromCmd(cmd))
			if err != nil {
				return fmt.Errorf("connecting to DSQL: %w", err)
			}
			defer out.DB.Close()

			planOut, err := planner.GeneratePlan(ctx, planner.GeneratePlanInput{
				DB:          out.DB,
				SchemaFiles: files,
			})
			if err != nil {
				return fmt.Errorf("generating plan: %w", err)
			}

			output.PrintPlan(planOut.Plan)
			return nil
		},
	}
}
