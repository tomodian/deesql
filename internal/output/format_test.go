package output

import (
	"bytes"
	"io"
	"os"
	"testing"

	"tomodian/dsql-migrate/internal/schema"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintPlan(t *testing.T) {
	// Disable color so ANSI codes don't interfere with string matching,
	// and all output goes through fmt to the captured stdout.
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = false })

	t.Run("empty plan", func(t *testing.T) {
		out := captureStdout(t, func() {
			PrintPlan(schema.Plan{})
		})
		assert.Contains(t, out, "No changes detected")
	})

	t.Run("plan with create statements", func(t *testing.T) {
		plan := schema.Plan{
			Statements: []schema.Statement{
				{DDL: "CREATE TABLE users (\n    id TEXT NOT NULL\n)", Action: schema.ActionCreate, Resource: "table.users"},
				{DDL: "CREATE INDEX ASYNC idx_users ON users (id)", Action: schema.ActionCreate, Resource: "index.idx_users"},
			},
		}

		out := captureStdout(t, func() {
			PrintPlan(plan)
		})
		assert.Contains(t, out, "2 statement(s)")
		assert.Contains(t, out, "CREATE TABLE users")
		assert.Contains(t, out, "CREATE INDEX ASYNC")
		assert.Contains(t, out, "+ table.users")
		assert.Contains(t, out, "2 to create")
	})

	t.Run("plan with hazards", func(t *testing.T) {
		plan := schema.Plan{
			Statements: []schema.Statement{
				{
					DDL:      "DROP TABLE users",
					Action:   schema.ActionDestroy,
					Resource: "table.users",
					Hazards:  []schema.Hazard{{Type: schema.HazardDeletesData, Message: "Drops all data"}},
				},
			},
		}

		out := captureStdout(t, func() {
			PrintPlan(plan)
		})
		assert.Contains(t, out, "DELETES_DATA")
		assert.Contains(t, out, "Drops all data")
		assert.Contains(t, out, "- table.users")
		assert.Contains(t, out, "1 to destroy")
	})

	t.Run("plan with all action types", func(t *testing.T) {
		plan := schema.Plan{
			Statements: []schema.Statement{
				{DDL: "CREATE TABLE a", Action: schema.ActionCreate, Resource: "table.a"},
				{DDL: "ALTER TABLE b", Action: schema.ActionUpdate, Resource: "table.b"},
				{DDL: "DROP TABLE c", Action: schema.ActionDestroy, Resource: "table.c"},
				{DDL: "DROP TABLE d; CREATE TABLE d", Action: schema.ActionReplace, Resource: "table.d"},
			},
		}

		out := captureStdout(t, func() {
			PrintPlan(plan)
		})
		assert.Contains(t, out, "+ table.a")
		assert.Contains(t, out, "~ table.b")
		assert.Contains(t, out, "- table.c")
		assert.Contains(t, out, "+/- table.d")
		assert.Contains(t, out, "1 to create")
		assert.Contains(t, out, "1 to update")
		assert.Contains(t, out, "1 to destroy")
		assert.Contains(t, out, "1 to replace")
		assert.Contains(t, out, "Legend:")
		assert.Contains(t, out, "+   Create")
		assert.Contains(t, out, "~   Update")
		assert.Contains(t, out, "-   Destroy")
		assert.Contains(t, out, "+/- Replace")
	})

	t.Run("unknown action shows question mark", func(t *testing.T) {
		plan := schema.Plan{
			Statements: []schema.Statement{
				{DDL: "SOMETHING", Action: "unknown", Resource: "table.x"},
			},
		}

		out := captureStdout(t, func() {
			PrintPlan(plan)
		})
		assert.Contains(t, out, "? table.x")
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	return buf.String()
}
