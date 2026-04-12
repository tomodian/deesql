package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"tomodian/deesql/internal/rules"
)

// Violation represents a single DSQL compatibility issue found in a SQL file.
type Violation struct {
	File    string
	Line    int
	Rule    string
	Context string
}

// verifyOnlyRules are rules specific to static .sql file verification.
var verifyOnlyRules = []rules.Rule{
	// Unsupported CREATE statements (not covered by shared rules)
	{Name: "CREATE PROCEDURE not supported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?PROCEDURE\b`)},
	{Name: "CREATE UNLOGGED TABLE not supported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+UNLOGGED\s+TABLE\b`)},
	{Name: "CREATE RULE not supported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?RULE\b`)},

	// Unsupported column types
	{Name: "SERIAL type not supported (use GENERATED AS IDENTITY)", Pattern: regexp.MustCompile(`(?i)\b(SMALL|BIG)?SERIAL\b`)},
	{Name: "money type not supported", Pattern: regexp.MustCompile(`(?i)\bmoney\b`)},
	{Name: "xml type not supported", Pattern: regexp.MustCompile(`(?i)\bxml\b`)},
	{Name: "cidr type not supported", Pattern: regexp.MustCompile(`(?i)\bcidr\b`)},
	{Name: "macaddr type not supported", Pattern: regexp.MustCompile(`(?i)\bmacaddr[0-9]*\b`)},
	{Name: "geometric types not supported (point, line, lseg, box, path, polygon, circle)", Pattern: regexp.MustCompile(`(?i)\b(point|line|lseg|box|path|polygon|circle)\b`)},
	{Name: "tsvector/tsquery types not supported", Pattern: regexp.MustCompile(`(?i)\b(tsvector|tsquery)\b`)},
	{Name: "range types not supported", Pattern: regexp.MustCompile(`(?i)\b(int4range|int8range|numrange|tsrange|tstzrange|daterange)\b`)},
	{Name: "hstore type not supported", Pattern: regexp.MustCompile(`(?i)\bhstore\b`)},
	{Name: "jsonb type not supported as column type (store as text, cast at query time)", Pattern: regexp.MustCompile(`(?i)\bjsonb\b`)},
	{Name: "json type not supported as column type (store as text, cast at query time)", Pattern: regexp.MustCompile(`(?i)\bjson\b`)},

	// Unsupported index types
	{Name: "only btree indexes supported (hash not supported)", Pattern: regexp.MustCompile(`(?i)\bUSING\s+hash\b`)},
	{Name: "only btree indexes supported (gin not supported)", Pattern: regexp.MustCompile(`(?i)\bUSING\s+gin\b`)},
	{Name: "only btree indexes supported (gist not supported)", Pattern: regexp.MustCompile(`(?i)\bUSING\s+gist\b`)},
	{Name: "only btree indexes supported (brin not supported)", Pattern: regexp.MustCompile(`(?i)\bUSING\s+brin\b`)},
	{Name: "only btree indexes supported (spgist not supported)", Pattern: regexp.MustCompile(`(?i)\bUSING\s+spgist\b`)},
	{Name: "CREATE INDEX CONCURRENTLY not supported (use CREATE INDEX ASYNC)", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\s+CONCURRENTLY\b`)},

	// Unsupported constraints
	{Name: "FOREIGN KEY constraint not supported", Pattern: regexp.MustCompile(`(?i)\bREFERENCES\s+\w+`)},
	{Name: "FOREIGN KEY constraint not supported", Pattern: regexp.MustCompile(`(?i)\bFOREIGN\s+KEY\b`)},
	{Name: "EXCLUDE constraint not supported", Pattern: regexp.MustCompile(`(?i)\bEXCLUDE\s+(USING\s+)?\(`)},
}

// allVerifyRules combines shared + verify-only rules.
var allVerifyRules = func() []rules.Rule {
	all := make([]rules.Rule, 0, len(rules.SharedRules)+len(verifyOnlyRules))
	all = append(all, rules.SharedRules...)
	all = append(all, verifyOnlyRules...)
	return all
}()

var (
	createIndexRe      = regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\s+`)
	createIndexAsyncRe = regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\s+ASYNC\b`)
)

// CheckDirs verifies all .sql files found in the given directories.
func CheckDirs(dirs []string) ([]Violation, error) {
	var files []string
	for _, dir := range dirs {
		if matches, err := filepath.Glob(filepath.Join(dir, "*.sql")); err != nil {
			return nil, fmt.Errorf("globbing %s: %w", dir, err)
		} else {
			files = append(files, matches...)
		}
	}
	return CheckFiles(files)
}

// CheckFiles verifies the given .sql file paths for DSQL compatibility.
func CheckFiles(files []string) ([]Violation, error) {
	var violations []Violation
	for _, f := range files {
		if data, err := os.ReadFile(f); err != nil {
			return nil, fmt.Errorf("reading %s: %w", f, err)
		} else {
			violations = append(violations, CheckSQL(f, string(data))...)
		}
	}
	return violations, nil
}

// CheckSQL checks raw SQL content for DSQL compatibility violations.
// The name parameter is used to populate the File field in returned Violations.
func CheckSQL(name, sql string) []Violation {
	lines := strings.Split(sql, "\n")
	var violations []Violation

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}

		for _, r := range allVerifyRules {
			if msg := r.Match(line); msg != "" {
				violations = append(violations, Violation{
					File:    name,
					Line:    i + 1,
					Rule:    msg,
					Context: trimmed,
				})
			}
		}

		// Check CREATE INDEX without ASYNC.
		if createIndexRe.MatchString(line) && !createIndexAsyncRe.MatchString(line) {
			violations = append(violations, Violation{
				File:    name,
				Line:    i + 1,
				Rule:    "CREATE INDEX must use ASYNC (use CREATE INDEX ASYNC)",
				Context: trimmed,
			})
		}
	}

	return violations
}
