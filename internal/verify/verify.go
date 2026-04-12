package verify

import (
	pkgverify "github.com/tomodian/deesql/pkg/verify"

	"github.com/tomodian/deesql/internal/ui"
)

// Violation is an alias for the public package type.
type Violation = pkgverify.Violation

// Files verifies all .sql files in the given directories.
func Files(schemaDirs []string) ([]Violation, error) {
	violations, err := pkgverify.CheckDirs(schemaDirs)
	if err != nil {
		return nil, err
	}
	printSummary(len(schemaDirs), violations)
	return violations, nil
}

// CheckFiles verifies the given .sql file paths.
func CheckFiles(files []string) ([]Violation, error) {
	ui.Step("Verifying DSQL compatibility for %d file(s)...", len(files))

	for _, f := range files {
		ui.Dim("    Checking %s\n", f)
	}

	violations, err := pkgverify.CheckFiles(files)
	if err != nil {
		return nil, err
	}

	if len(violations) == 0 {
		ui.Success("All %d file(s) passed", len(files))
	} else {
		ui.Warn("%d issue(s) found in %d file(s)", len(violations), len(files))
	}
	return violations, nil
}

func printSummary(count int, violations []Violation) {
	if len(violations) == 0 {
		ui.Success("All %d dir(s) passed", count)
	} else {
		ui.Warn("%d issue(s) found in %d dir(s)", len(violations), count)
	}
}
