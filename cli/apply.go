package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tomodian/deesql/internal/dsqlconn"
	"github.com/tomodian/deesql/internal/output"
	"github.com/tomodian/deesql/internal/planner"
	"github.com/tomodian/deesql/internal/runner"

	"github.com/urfave/cli/v3"
)

func applyCmd() *cli.Command {
	return &cli.Command{
		Name:  "apply",
		Usage: "Generate and apply a migration plan",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:  FlagAllowHazards,
				Usage: "Hazard types to permit (e.g. INDEX_BUILD,DELETES_DATA)",
			},
			&cli.BoolFlag{
				Name:  FlagForce,
				Usage: "Apply without confirmation prompt",
				Value: false,
			},
			&cli.IntFlag{
				Name:  FlagRetries,
				Usage: "Max retries on OCC conflict (SQLSTATE 40001)",
				Value: DefaultRetries,
			},
			&cli.DurationFlag{
				Name:  FlagRetryDelay,
				Usage: "Initial delay between retries (doubles each attempt)",
				Value: DefaultRetryDelay,
			},
		},
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

			if len(planOut.Plan.Statements) == 0 {
				fmt.Println("Schema is up to date. No changes to apply.")
				return nil
			}

			output.PrintPlan(planOut.Plan)

			if err := runner.CheckHazards(ctx, runner.CheckHazardsInput{
				Plan:           planOut.Plan,
				AllowedHazards: cmd.StringSlice(FlagAllowHazards),
			}); err != nil {
				return err
			}

			if !cmd.Bool(FlagForce) {
				fmt.Print("\nApply this migration? [y/N]: ")
				reader := bufio.NewReader(os.Stdin)
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					fmt.Println("Migration cancelled.")
					return nil
				}
			}

			if err := runner.Execute(ctx, runner.ExecuteInput{
				DB:             out.DB,
				Plan:           planOut.Plan,
				MaxRetries:     int(cmd.Int(FlagRetries)),
				RetryBaseDelay: cmd.Duration(FlagRetryDelay),
			}); err != nil {
				return fmt.Errorf("applying migration: %w", err)
			}

			fmt.Println("Migration applied successfully.")
			return nil
		},
	}
}
