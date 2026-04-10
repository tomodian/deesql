package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tomodian/deesql/internal/dsqlconn"
	"tomodian/deesql/internal/verify"

	"github.com/urfave/cli/v3"
)

func connectInputFromCmd(cmd *cli.Command) dsqlconn.ConnectInput {
	return dsqlconn.ConnectInput{
		Endpoint:       cmd.String(FlagEndpoint),
		Region:         cmd.String(FlagRegion),
		User:           cmd.String(FlagUser),
		Profile:        cmd.String(FlagProfile),
		RoleARN:        cmd.String(FlagRoleARN),
		ConnectTimeout: cmd.Duration(FlagConnectTimeout),
	}
}

func requireFlags(cmd *cli.Command) error {
	if cmd.String(FlagEndpoint) == "" {
		return fmt.Errorf("--%s is required", FlagEndpoint)
	}
	if len(cmd.StringSlice(FlagSchema)) == 0 {
		return fmt.Errorf("--%s is required", FlagSchema)
	}
	return nil
}

// resolveSchemaFiles resolves --schema values to a list of .sql file paths.
// Each value can be a .sql file or a directory containing .sql files.
func resolveSchemaFiles(paths []string) ([]string, error) {
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("--%s %s: %w", FlagSchema, p, err)
		}

		if !info.IsDir() {
			if !strings.HasSuffix(strings.ToLower(p), ".sql") {
				return nil, fmt.Errorf("--%s %s: not a .sql file", FlagSchema, p)
			}
			files = append(files, p)
			continue
		}

		matches, err := filepath.Glob(filepath.Join(p, "*.sql"))
		if err != nil {
			return nil, fmt.Errorf("reading --%s %s: %w", FlagSchema, p, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no .sql files found in --%s %s", FlagSchema, p)
		}
		files = append(files, matches...)
	}
	return files, nil
}

func verifySchemaFiles(files []string) error {
	violations, err := verify.CheckFiles(files)
	if err != nil {
		return fmt.Errorf("verifying schemas: %w", err)
	}

	if len(violations) == 0 {
		return nil
	}

	fmt.Fprintf(os.Stderr, "Schema files contain %d DSQL compatibility issue(s):\n\n", len(violations))
	for _, v := range violations {
		fmt.Fprintf(os.Stderr, "  %s:%d\n", v.File, v.Line)
		fmt.Fprintf(os.Stderr, "    Rule: %s\n", v.Rule)
		fmt.Fprintf(os.Stderr, "    Line: %s\n\n", v.Context)
	}

	return fmt.Errorf("fix %d schema issue(s) before running plan/apply", len(violations))
}
