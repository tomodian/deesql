package output

import (
	"fmt"
	"strings"

	"tomodian/dsql-migrate/internal/schema"

	"github.com/fatih/color"
)

var (
	green  = color.New(color.FgGreen)
	yellow = color.New(color.FgYellow)
	red    = color.New(color.FgRed)
	cyan   = color.New(color.FgCyan)
	dim    = color.New(color.FgHiBlack)
	bold   = color.New(color.Bold)
)

// PrintPlan displays a migration plan to stdout with colored Terraform-style summary.
func PrintPlan(plan schema.Plan) {
	if len(plan.Statements) == 0 {
		green.Println("No changes detected. Schema is up to date.")
		return
	}

	fmt.Println()
	bold.Printf("Migration plan (%d statement(s)):\n", len(plan.Statements))
	fmt.Println(strings.Repeat("-", 60))

	for i, stmt := range plan.Statements {
		fmt.Println()
		actionColor(stmt.Action).Printf("  %s %s\n", actionLabel(stmt.Action), stmt.Resource)
		dim.Printf("    -- Statement %d\n", i+1)
		fmt.Printf("    %s\n", stmt.ToSQL())

		for _, h := range stmt.Hazards {
			yellow.Printf("    -- ⚠ %s: %s\n", h.Type, h.Message)
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("-", 60))
	printSummary(plan)
}

func actionLabel(a schema.Action) string {
	switch a {
	case schema.ActionCreate:
		return "+"
	case schema.ActionUpdate:
		return "~"
	case schema.ActionDestroy:
		return "-"
	case schema.ActionReplace:
		return "+/-"
	default:
		return "?"
	}
}

func actionColor(a schema.Action) *color.Color {
	switch a {
	case schema.ActionCreate:
		return green
	case schema.ActionUpdate:
		return yellow
	case schema.ActionDestroy:
		return red
	case schema.ActionReplace:
		return cyan
	default:
		return dim
	}
}

func printSummary(plan schema.Plan) {
	counts := plan.Summary()

	var parts []string
	if n := counts[schema.ActionCreate]; n > 0 {
		parts = append(parts, green.Sprintf("%d to create", n))
	}
	if n := counts[schema.ActionUpdate]; n > 0 {
		parts = append(parts, yellow.Sprintf("%d to update", n))
	}
	if n := counts[schema.ActionDestroy]; n > 0 {
		parts = append(parts, red.Sprintf("%d to destroy", n))
	}
	if n := counts[schema.ActionReplace]; n > 0 {
		parts = append(parts, cyan.Sprintf("%d to replace", n))
	}

	bold.Print("Plan: ")
	fmt.Printf("%s.\n", strings.Join(parts, ", "))

	fmt.Println()
	dim.Println("Legend:")
	if counts[schema.ActionCreate] > 0 {
		green.Println("  +   Create")
	}
	if counts[schema.ActionUpdate] > 0 {
		yellow.Println("  ~   Update")
	}
	if counts[schema.ActionDestroy] > 0 {
		red.Println("  -   Destroy")
	}
	if counts[schema.ActionReplace] > 0 {
		cyan.Println("  +/- Replace")
	}
}
