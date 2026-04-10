package output

import (
	"fmt"
	"strings"

	"tomodian/deesql/internal/schema"

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
		fmt.Println(green.Sprint("No changes detected. Schema is up to date."))
		return
	}

	fmt.Println()
	fmt.Println(bold.Sprintf("Migration plan (%d statement(s)):", len(plan.Statements)))
	fmt.Println(strings.Repeat("-", 60))

	for i, stmt := range plan.Statements {
		fmt.Println()
		fmt.Println(actionColor(stmt.Action).Sprintf("  %s %s", actionLabel(stmt.Action), stmt.Resource))
		fmt.Println(dim.Sprintf("    -- Statement %d", i+1))
		fmt.Printf("    %s\n", stmt.ToSQL())

		for _, h := range stmt.Hazards {
			fmt.Println(yellow.Sprintf("    -- ⚠ %s: %s", h.Type, h.Message))
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

	fmt.Printf("%s%s.\n", bold.Sprint("Plan: "), strings.Join(parts, ", "))

	fmt.Println()
	fmt.Println(dim.Sprint("Legend:"))
	if counts[schema.ActionCreate] > 0 {
		fmt.Println(green.Sprint("  +   Create"))
	}
	if counts[schema.ActionUpdate] > 0 {
		fmt.Println(yellow.Sprint("  ~   Update"))
	}
	if counts[schema.ActionDestroy] > 0 {
		fmt.Println(red.Sprint("  -   Destroy"))
	}
	if counts[schema.ActionReplace] > 0 {
		fmt.Println(cyan.Sprint("  +/- Replace"))
	}
}
