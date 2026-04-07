package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"tomodian/dsql-migrate/internal/ui"
)

type Violation struct {
	File    string
	Line    int
	Rule    string
	Context string
}

type rule struct {
	name    string
	pattern *regexp.Regexp
}

var (
	rules = []rule{
		// Unsupported CREATE statements
		{"CREATE EXTENSION not supported", regexp.MustCompile(`(?i)\bCREATE\s+EXTENSION\b`)},
		{"CREATE TRIGGER not supported", regexp.MustCompile(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?TRIGGER\b`)},
		{"CREATE PROCEDURE not supported", regexp.MustCompile(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?PROCEDURE\b`)},
		{"CREATE TYPE not supported", regexp.MustCompile(`(?i)\bCREATE\s+TYPE\b`)},
		{"CREATE DATABASE not supported", regexp.MustCompile(`(?i)\bCREATE\s+DATABASE\b`)},
		{"CREATE TABLESPACE not supported", regexp.MustCompile(`(?i)\bCREATE\s+TABLESPACE\b`)},
		{"CREATE TEMP TABLE not supported", regexp.MustCompile(`(?i)\bCREATE\s+(GLOBAL\s+|LOCAL\s+)?TEMP(ORARY)?\s+TABLE\b`)},
		{"CREATE UNLOGGED TABLE not supported", regexp.MustCompile(`(?i)\bCREATE\s+UNLOGGED\s+TABLE\b`)},
		{"CREATE MATERIALIZED VIEW not supported", regexp.MustCompile(`(?i)\bCREATE\s+MATERIALIZED\s+VIEW\b`)},
		{"CREATE RULE not supported", regexp.MustCompile(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?RULE\b`)},

		// Functions: only LANGUAGE SQL is supported
		{"CREATE FUNCTION with non-SQL language not supported (only LANGUAGE SQL allowed)", regexp.MustCompile(`(?i)\bLANGUAGE\s+(plpgsql|plv8|plperl|plpython|plpython3u|pltcl|c)\b`)},

		// Unsupported column types
		{"SERIAL type not supported (use GENERATED AS IDENTITY)", regexp.MustCompile(`(?i)\b(SMALL|BIG)?SERIAL\b`)},
		{"money type not supported", regexp.MustCompile(`(?i)\bmoney\b`)},
		{"xml type not supported", regexp.MustCompile(`(?i)\bxml\b`)},
		{"cidr type not supported", regexp.MustCompile(`(?i)\bcidr\b`)},
		{"macaddr type not supported", regexp.MustCompile(`(?i)\bmacaddr[0-9]*\b`)},
		{"geometric types not supported (point, line, lseg, box, path, polygon, circle)", regexp.MustCompile(`(?i)\b(point|line|lseg|box|path|polygon|circle)\b`)},
		{"tsvector/tsquery types not supported", regexp.MustCompile(`(?i)\b(tsvector|tsquery)\b`)},
		{"range types not supported", regexp.MustCompile(`(?i)\b(int4range|int8range|numrange|tsrange|tstzrange|daterange)\b`)},
		{"hstore type not supported", regexp.MustCompile(`(?i)\bhstore\b`)},

		// Unsupported index types
		{"only btree indexes supported (hash not supported)", regexp.MustCompile(`(?i)\bUSING\s+hash\b`)},
		{"only btree indexes supported (gin not supported)", regexp.MustCompile(`(?i)\bUSING\s+gin\b`)},
		{"only btree indexes supported (gist not supported)", regexp.MustCompile(`(?i)\bUSING\s+gist\b`)},
		{"only btree indexes supported (brin not supported)", regexp.MustCompile(`(?i)\bUSING\s+brin\b`)},
		{"only btree indexes supported (spgist not supported)", regexp.MustCompile(`(?i)\bUSING\s+spgist\b`)},
		{"CREATE INDEX CONCURRENTLY not supported (use CREATE INDEX ASYNC)", regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\s+CONCURRENTLY\b`)},

		// Unsupported constraints
		{"FOREIGN KEY constraint not supported", regexp.MustCompile(`(?i)\bREFERENCES\s+\w+`)},
		{"FOREIGN KEY constraint not supported", regexp.MustCompile(`(?i)\bFOREIGN\s+KEY\b`)},
		{"EXCLUDE constraint not supported", regexp.MustCompile(`(?i)\bEXCLUDE\s+(USING\s+)?\(`)},

		// Unsupported table options
		{"PARTITION BY not supported (Aurora DSQL auto-partitions)", regexp.MustCompile(`(?i)\bPARTITION\s+BY\b`)},
		{"INHERITS not supported", regexp.MustCompile(`(?i)\bINHERITS\s*\(`)},

		// Unsupported statements
		{"TRUNCATE not supported (use DELETE FROM)", regexp.MustCompile(`(?i)\bTRUNCATE\b`)},
	}

	// CREATE INDEX without ASYNC: matched separately because Go regexp
	// doesn't support negative lookahead.
	createIndexRe      = regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\s+`)
	createIndexAsyncRe = regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\s+ASYNC\b`)
)

// Files verifies all .sql files in the given directories.
func Files(schemaDirs []string) ([]Violation, error) {
	var files []string
	for _, dir := range schemaDirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*.sql"))
		if err != nil {
			return nil, fmt.Errorf("globbing %s: %w", dir, err)
		}
		files = append(files, matches...)
	}
	return CheckFiles(files)
}

// CheckFiles verifies the given .sql file paths.
func CheckFiles(files []string) ([]Violation, error) {
	ui.Step("Verifying DSQL compatibility for %d file(s)...", len(files))
	var violations []Violation

	for _, f := range files {
		ui.Dim("    Checking %s\n", f)
		vs, err := checkFile(f)
		if err != nil {
			return nil, err
		}
		violations = append(violations, vs...)
	}

	if len(violations) == 0 {
		ui.Success("All %d file(s) passed", len(files))
	} else {
		ui.Warn("%d issue(s) found in %d file(s)", len(violations), len(files))
	}
	return violations, nil
}

func checkFile(path string) ([]Violation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	lines := strings.Split(string(data), "\n")
	var violations []Violation

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}

		for _, r := range rules {
			if r.pattern.MatchString(line) {
				violations = append(violations, Violation{
					File:    path,
					Line:    i + 1,
					Rule:    r.name,
					Context: trimmed,
				})
			}
		}

		// Check CREATE INDEX without ASYNC.
		if createIndexRe.MatchString(line) && !createIndexAsyncRe.MatchString(line) {
			violations = append(violations, Violation{
				File:    path,
				Line:    i + 1,
				Rule:    "CREATE INDEX must use ASYNC (use CREATE INDEX ASYNC)",
				Context: trimmed,
			})
		}
	}

	return violations, nil
}
