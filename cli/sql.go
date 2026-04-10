package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"tomodian/deesql/internal/dsqlconn"
	"tomodian/deesql/internal/ui"

	"github.com/urfave/cli/v3"
)

func sqlCmd() *cli.Command {
	return &cli.Command{
		Name:      "sql",
		Usage:     "Execute raw SQL file against Aurora DSQL",
		ArgsUsage: "<file.sql>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.String(FlagEndpoint) == "" {
				return fmt.Errorf("--%s is required", FlagEndpoint)
			}
			if cmd.NArg() == 0 {
				return fmt.Errorf("SQL file argument is required")
			}

			filePath := cmd.Args().First()
			data, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("reading %s: %w", filePath, err)
			}

			out, err := dsqlconn.Connect(ctx, connectInputFromCmd(cmd))
			if err != nil {
				return fmt.Errorf("connecting to DSQL: %w", err)
			}
			defer out.DB.Close()

			stmts := splitSQL(string(data))
			ui.Info("Executing %d statement(s) from %s", len(stmts), filePath)

			for i, stmt := range stmts {
				ui.Dim("  (%d/%d) %s\n", i+1, len(stmts), truncateStmt(stmt))
				if _, err := out.DB.ExecContext(ctx, stmt); err != nil {
					ui.Error("Statement %d failed: %v", i+1, err)
					return fmt.Errorf("statement %d failed: %w", i+1, err)
				}
			}

			ui.Success("All %d statement(s) executed successfully.", len(stmts))
			return nil
		},
	}
}

// splitSQL splits raw SQL on semicolons, skipping empty statements and comments.
func splitSQL(sql string) []string {
	var stmts []string
	for _, raw := range strings.Split(sql, ";") {
		// Strip comment-only lines.
		var lines []string
		for _, line := range strings.Split(raw, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "--") {
				lines = append(lines, line)
			}
		}
		stmt := strings.TrimSpace(strings.Join(lines, "\n"))
		if stmt != "" {
			stmts = append(stmts, stmt)
		}
	}
	return stmts
}

func truncateStmt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}
