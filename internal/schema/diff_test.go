package schema

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiff(t *testing.T) {
	t.Run("no changes", func(t *testing.T) {
		s := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text", IsNullable: false}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: s, Desired: s})
		require.NoError(t, err)
		assert.Empty(t, out.Plan.Statements)
	})

	t.Run("create new table", func(t *testing.T) {
		current := Schema{}
		desired := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text", IsNullable: false}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Contains(t, out.Plan.Statements[0].DDL, "CREATE TABLE users")
	})

	t.Run("drop table", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:    "old_table",
			Columns: []Column{{Name: "id", Type: "text"}},
		}}}
		desired := Schema{}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Contains(t, out.Plan.Statements[0].DDL, "DROP TABLE old_table")
		require.Len(t, out.Plan.Statements[0].Hazards, 1)
		assert.Equal(t, HazardDeletesData, out.Plan.Statements[0].Hazards[0].Type)
	})

	t.Run("add column", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text", IsNullable: false}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name: "users",
			Columns: []Column{
				{Name: "id", Type: "text", IsNullable: false},
				{Name: "email", Type: "text", IsNullable: true},
			},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Contains(t, out.Plan.Statements[0].DDL, "ADD COLUMN email")
	})

	t.Run("drop column returns error", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name: "users",
			Columns: []Column{
				{Name: "id", Type: "text"},
				{Name: "email", Type: "text"},
			},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}

		_, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "DROP COLUMN not supported")
	})

	t.Run("alter column type returns error", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "integer"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}

		_, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ALTER COLUMN TYPE not supported")
	})

	t.Run("alter nullability returns error", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text", IsNullable: true}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text", IsNullable: false}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}

		_, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SET/DROP NOT NULL not supported")
	})

	t.Run("primary key change returns error", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name: "users",
			Columns: []Column{
				{Name: "id", Type: "text"},
				{Name: "code", Type: "text"},
			},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name: "users",
			Columns: []Column{
				{Name: "id", Type: "text"},
				{Name: "code", Type: "text"},
			},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"code"}},
		}}}

		_, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "PRIMARY KEY change not supported")
	})

	t.Run("add unique constraint", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "email", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "email", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
			UniqueConstraints: []UniqueConstraint{
				{Name: "users_email_key", Columns: []string{"email"}},
			},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Contains(t, out.Plan.Statements[0].DDL, "ADD CONSTRAINT users_email_key UNIQUE")
	})

	t.Run("drop unique constraint", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "email", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
			UniqueConstraints: []UniqueConstraint{
				{Name: "users_email_key", Columns: []string{"email"}},
			},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "email", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Contains(t, out.Plan.Statements[0].DDL, "DROP CONSTRAINT users_email_key")
	})

	t.Run("add and drop index", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "email", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
			Indexes:    []Index{{Name: "idx_old", Columns: []string{"email"}}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "email", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
			Indexes:    []Index{{Name: "idx_new", Columns: []string{"id", "email"}}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)

		var ddls []string
		for _, s := range out.Plan.Statements {
			ddls = append(ddls, s.DDL)
		}
		assert.Contains(t, ddls[0], "DROP INDEX idx_old")
		assert.Contains(t, ddls[1], "CREATE")
		assert.Contains(t, ddls[1], "idx_new")
	})

	t.Run("recreate index when columns change", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "t",
			Columns:    []Column{{Name: "a", Type: "text"}, {Name: "b", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "t_pkey", Columns: []string{"a"}},
			Indexes:    []Index{{Name: "idx_t_a", Columns: []string{"a"}}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "t",
			Columns:    []Column{{Name: "a", Type: "text"}, {Name: "b", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "t_pkey", Columns: []string{"a"}},
			Indexes:    []Index{{Name: "idx_t_a", Columns: []string{"a", "b"}}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 2)
		assert.Contains(t, out.Plan.Statements[0].DDL, "DROP INDEX idx_t_a")
		assert.Contains(t, out.Plan.Statements[1].DDL, "CREATE")
	})

	t.Run("create table with indexes", func(t *testing.T) {
		current := Schema{}
		desired := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "email", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
			Indexes:    []Index{{Name: "idx_email", Columns: []string{"email"}}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 2)
		assert.Contains(t, out.Plan.Statements[0].DDL, "CREATE TABLE")
		assert.Contains(t, out.Plan.Statements[1].DDL, "CREATE")
		assert.Contains(t, out.Plan.Statements[1].DDL, "INDEX")
	})

	t.Run("statement ordering", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "keep",
			Columns:    []Column{{Name: "id", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "keep_pkey", Columns: []string{"id"}},
			Indexes:    []Index{{Name: "idx_old", Columns: []string{"id"}}},
		}, {
			Name:    "remove",
			Columns: []Column{{Name: "id", Type: "text"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "keep",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "new_col", Type: "text", IsNullable: true}},
			PrimaryKey: &PrimaryKey{Name: "keep_pkey", Columns: []string{"id"}},
		}, {
			Name:       "fresh",
			Columns:    []Column{{Name: "id", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "fresh_pkey", Columns: []string{"id"}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)

		// Should have: DROP INDEX, DROP TABLE, CREATE TABLE, ADD COLUMN
		require.True(t, len(out.Plan.Statements) >= 4)

		// Verify ordering: drops before creates
		foundDropIndex := false
		foundDropTable := false
		foundCreate := false
		for _, s := range out.Plan.Statements {
			if !foundDropIndex && containsStr(s.DDL, "DROP INDEX") {
				foundDropIndex = true
			}
			if foundDropIndex && !foundDropTable && containsStr(s.DDL, "DROP TABLE") {
				foundDropTable = true
			}
			if foundDropTable && !foundCreate && containsStr(s.DDL, "CREATE TABLE") {
				foundCreate = true
			}
		}
		assert.True(t, foundDropIndex, "should have DROP INDEX")
		assert.True(t, foundDropTable, "should have DROP TABLE")
		assert.True(t, foundCreate, "should have CREATE TABLE")
	})

	t.Run("add PK where none existed returns error", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:    "t",
			Columns: []Column{{Name: "id", Type: "text"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "t",
			Columns:    []Column{{Name: "id", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "t_pkey", Columns: []string{"id"}},
		}}}

		_, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "PRIMARY KEY change not supported")
	})

	t.Run("remove PK returns error", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "t",
			Columns:    []Column{{Name: "id", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "t_pkey", Columns: []string{"id"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:    "t",
			Columns: []Column{{Name: "id", Type: "text"}},
		}}}

		_, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "PRIMARY KEY change not supported")
	})

	t.Run("add check constraint", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "t",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "val", Type: "integer"}},
			PrimaryKey: &PrimaryKey{Name: "t_pkey", Columns: []string{"id"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "t",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "val", Type: "integer"}},
			PrimaryKey: &PrimaryKey{Name: "t_pkey", Columns: []string{"id"}},
			CheckConstraints: []CheckConstraint{
				{Name: "t_val_check", Expression: "(val > 0)"},
			},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Contains(t, out.Plan.Statements[0].DDL, "ADD CONSTRAINT t_val_check CHECK")
	})

	t.Run("drop check constraint", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "t",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "val", Type: "integer"}},
			PrimaryKey: &PrimaryKey{Name: "t_pkey", Columns: []string{"id"}},
			CheckConstraints: []CheckConstraint{
				{Name: "t_val_check", Expression: "(val > 0)"},
			},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "t",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "val", Type: "integer"}},
			PrimaryKey: &PrimaryKey{Name: "t_pkey", Columns: []string{"id"}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Contains(t, out.Plan.Statements[0].DDL, "DROP CONSTRAINT t_val_check")
	})

	t.Run("alter default returns error", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "t",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "val", Type: "text", Default: "'old'"}},
			PrimaryKey: &PrimaryKey{Name: "t_pkey", Columns: []string{"id"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "t",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "val", Type: "text", Default: "'new'"}},
			PrimaryKey: &PrimaryKey{Name: "t_pkey", Columns: []string{"id"}},
		}}}

		_, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "SET/DROP DEFAULT not supported")
	})

	t.Run("both no PK is fine", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:    "t",
			Columns: []Column{{Name: "id", Type: "text"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:    "t",
			Columns: []Column{{Name: "id", Type: "text"}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		assert.Empty(t, out.Plan.Statements)
	})
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstr(s, substr))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
