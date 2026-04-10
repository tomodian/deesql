package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"tomodian/deesql/internal/dsqlconn"
	"tomodian/deesql/internal/ui"

	"github.com/urfave/cli/v3"
)

func sqlCmd() *cli.Command {
	return &cli.Command{
		Name:      "sql",
		Usage:     "Execute raw SQL file against Aurora DSQL",
		ArgsUsage: "<file.sql>",
		Flags: []cli.Flag{
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

			maxRetries := int(cmd.Int(FlagRetries))
			retryDelay := cmd.Duration(FlagRetryDelay)

			for i, stmt := range stmts {
				ui.Dim("  (%d/%d) %s\n", i+1, len(stmts), truncateStmt(stmt))
				if err := execWithRetry(ctx, out, stmt, i+1, maxRetries, retryDelay); err != nil {
					return err
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

func execWithRetry(ctx context.Context, out *dsqlconn.ConnectOutput, stmt string, num int, maxRetries int, baseDelay time.Duration) error {
	delay := baseDelay

	for attempt := 0; attempt <= maxRetries; attempt++ {
		_, err := out.DB.ExecContext(ctx, stmt)
		if err == nil {
			return nil
		}

		if strings.Contains(err.Error(), "40001") && attempt < maxRetries {
			ui.Warn("Statement %d: OCC conflict, retrying in %s (%d/%d)...", num, delay, attempt+1, maxRetries)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			delay *= 2
			continue
		}

		ui.Error("Statement %d failed: %v", num, err)
		return fmt.Errorf("statement %d failed: %w", num, err)
	}
	return nil
}

func truncateStmt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}
