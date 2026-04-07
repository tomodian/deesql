package cli

import (
	"context"
	"fmt"
	"os"

	"tomodian/dsql-migrate/internal/verify"

	"github.com/urfave/cli/v3"
)

func verifyCmd() *cli.Command {
	return &cli.Command{
		Name:  "verify",
		Usage: "Check schema SQL files for Aurora DSQL compatibility",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			schemas := cmd.StringSlice(FlagSchema)
			if len(schemas) == 0 {
				return fmt.Errorf("--%s is required", FlagSchema)
			}

			files, err := resolveSchemaFiles(schemas)
			if err != nil {
				return err
			}

			violations, err := verify.CheckFiles(files)
			if err != nil {
				return fmt.Errorf("verifying schemas: %w", err)
			}

			if len(violations) == 0 {
				fmt.Println("All schema files are compatible with Aurora DSQL.")
				return nil
			}

			fmt.Fprintf(os.Stderr, "Found %d compatibility issue(s):\n\n", len(violations))
			for _, v := range violations {
				fmt.Fprintf(os.Stderr, "  %s:%d\n", v.File, v.Line)
				fmt.Fprintf(os.Stderr, "    Rule: %s\n", v.Rule)
				fmt.Fprintf(os.Stderr, "    Line: %s\n\n", v.Context)
			}

			return fmt.Errorf("%d DSQL compatibility issue(s) found", len(violations))
		},
	}
}
