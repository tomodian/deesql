package schema

import (
	"context"
	"math"
	"strings"
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

	t.Run("drop column", func(t *testing.T) {
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

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Equal(t, "ALTER TABLE users DROP COLUMN email", out.Plan.Statements[0].DDL)
		assert.Equal(t, ActionDestroy, out.Plan.Statements[0].Action)
		require.Len(t, out.Plan.Statements[0].Hazards, 1)
		assert.Equal(t, HazardDeletesData, out.Plan.Statements[0].Hazards[0].Type)
	})

	t.Run("drop primary key column returns error", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "code", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id", "code"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "text"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id", "code"}},
		}}}

		_, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "primary key column")
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

	t.Run("set not null returns error", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:    "users",
			Columns: []Column{{Name: "id", Type: "text"}, {Name: "email", Type: "text", IsNullable: true}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:    "users",
			Columns: []Column{{Name: "id", Type: "text"}, {Name: "email", Type: "text", IsNullable: false}},
		}}}

		_, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SET NOT NULL not supported")
	})

	t.Run("drop not null", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:    "users",
			Columns: []Column{{Name: "email", Type: "text", IsNullable: false}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:    "users",
			Columns: []Column{{Name: "email", Type: "text", IsNullable: true}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Equal(t, "ALTER TABLE users ALTER COLUMN email DROP NOT NULL", out.Plan.Statements[0].DDL)
		assert.Equal(t, ActionUpdate, out.Plan.Statements[0].Action)
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

	t.Run("add unique constraint via unique index", func(t *testing.T) {
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
				{Name: "users_email_key", Columns: []string{"email"}, NullsNotDistinct: true},
			},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 2)
		assert.Equal(t, "CREATE UNIQUE INDEX ASYNC users_email_key ON users (email) NULLS NOT DISTINCT", out.Plan.Statements[0].DDL)
		assert.True(t, out.Plan.Statements[0].WaitForJob)
		assert.Equal(t, HazardIndexBuild, out.Plan.Statements[0].Hazards[0].Type)
		assert.Equal(t, "ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE USING INDEX users_email_key", out.Plan.Statements[1].DDL)
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
		assert.Equal(t, "ALTER TABLE users DROP CONSTRAINT users_email_key", out.Plan.Statements[0].DDL)
		assert.Equal(t, HazardCorrectness, out.Plan.Statements[0].Hazards[0].Type)
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

	t.Run("add check constraint with NOT VALID then validate", func(t *testing.T) {
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
		require.Len(t, out.Plan.Statements, 2)
		assert.Equal(t, "ALTER TABLE t ADD CONSTRAINT t_val_check CHECK (val > 0) NOT VALID", out.Plan.Statements[0].DDL)
		assert.Equal(t, "ALTER TABLE ASYNC t VALIDATE CONSTRAINT t_val_check", out.Plan.Statements[1].DDL)
		assert.Equal(t, HazardConstraintValidation, out.Plan.Statements[1].Hazards[0].Type)
	})

	t.Run("not-valid check constraint is validated", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:             "t",
			Columns:          []Column{{Name: "val", Type: "integer"}},
			CheckConstraints: []CheckConstraint{{Name: "t_val_check", Expression: "((val > 0))", NotValid: true}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:             "t",
			Columns:          []Column{{Name: "val", Type: "integer"}},
			CheckConstraints: []CheckConstraint{{Name: "t_val_check", Expression: "(val > 0)"}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Equal(t, "ALTER TABLE ASYNC t VALIDATE CONSTRAINT t_val_check", out.Plan.Statements[0].DDL)
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
		assert.Equal(t, "ALTER TABLE t DROP CONSTRAINT t_val_check", out.Plan.Statements[0].DDL)
	})

	t.Run("set default", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:       "t",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "val", Type: "text", Default: "'old'::text"}},
			PrimaryKey: &PrimaryKey{Name: "t_pkey", Columns: []string{"id"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:       "t",
			Columns:    []Column{{Name: "id", Type: "text"}, {Name: "val", Type: "text", Default: "'new'"}},
			PrimaryKey: &PrimaryKey{Name: "t_pkey", Columns: []string{"id"}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Equal(t, "ALTER TABLE t ALTER COLUMN val SET DEFAULT 'new'", out.Plan.Statements[0].DDL)
	})

	t.Run("drop default", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:    "t",
			Columns: []Column{{Name: "val", Type: "text", Default: "'old'::text"}},
		}}}
		desired := Schema{Tables: []Table{{
			Name:    "t",
			Columns: []Column{{Name: "val", Type: "text"}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Equal(t, "ALTER TABLE t ALTER COLUMN val DROP DEFAULT", out.Plan.Statements[0].DDL)
	})

	t.Run("unchanged default with cast is not a diff", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "val", Type: "text", Default: "'x'::text"}}}}}
		desired := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "val", Type: "text", Default: "'x'"}}}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		assert.Empty(t, out.Plan.Statements)
	})

	t.Run("numeric equals numeric(18,6)", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "n", Type: "numeric(18,6)", IsNullable: true}}}}}
		desired := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "n", Type: "numeric", IsNullable: true}}}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		assert.Empty(t, out.Plan.Statements)
	})

	t.Run("identity changes", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "t", Columns: []Column{
			{Name: "a", Type: "bigint"},
			{Name: "b", Type: "bigint", IsIdentity: true, IdentityGeneration: "ALWAYS"},
			{Name: "c", Type: "bigint", IsIdentity: true, IdentityGeneration: "ALWAYS"},
		}}}}
		desired := Schema{Tables: []Table{{Name: "t", Columns: []Column{
			{Name: "a", Type: "bigint", IsIdentity: true, IdentityGeneration: "BY DEFAULT", IdentityOptions: "CACHE 65536"},
			{Name: "b", Type: "bigint"},
			{Name: "c", Type: "bigint", IsIdentity: true, IdentityGeneration: "BY DEFAULT"},
		}}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		var ddls []string
		for _, s := range out.Plan.Statements {
			ddls = append(ddls, s.DDL)
		}
		assert.Equal(t, []string{
			"ALTER TABLE t ALTER COLUMN a ADD GENERATED BY DEFAULT AS IDENTITY (CACHE 65536)",
			"ALTER TABLE t ALTER COLUMN b DROP IDENTITY",
			"ALTER TABLE t ALTER COLUMN c SET GENERATED BY DEFAULT",
		}, ddls)
	})

	t.Run("add identity requires bigint and CACHE", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "a", Type: "integer"}, {Name: "b", Type: "bigint"}}}}}

		_, err := Diff(context.Background(), DiffInput{Current: current, Desired: Schema{Tables: []Table{{Name: "t", Columns: []Column{
			{Name: "a", Type: "integer", IsIdentity: true, IdentityGeneration: "ALWAYS", IdentityOptions: "CACHE 1"},
			{Name: "b", Type: "bigint"},
		}}}}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be bigint")

		_, err = Diff(context.Background(), DiffInput{Current: current, Desired: Schema{Tables: []Table{{Name: "t", Columns: []Column{
			{Name: "a", Type: "integer"},
			{Name: "b", Type: "bigint", IsIdentity: true, IdentityGeneration: "ALWAYS"},
		}}}}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "explicit CACHE")
	})

	t.Run("drop expression of generated column", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "g", Type: "integer", IsNullable: true, Generated: "(a * 2)"}}}}}
		desired := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "g", Type: "integer", IsNullable: true}}}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Equal(t, "ALTER TABLE t ALTER COLUMN g DROP EXPRESSION", out.Plan.Statements[0].DDL)
	})

	t.Run("adding generation expression returns error", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "g", Type: "integer", IsNullable: true}}}}}
		desired := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "g", Type: "integer", IsNullable: true, Generated: "(a * 2)"}}}}}

		_, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "generation expression")
	})

	t.Run("create tables ordered by foreign keys", func(t *testing.T) {
		desired := Schema{Tables: []Table{{
			Name:        "orders",
			Columns:     []Column{{Name: "id", Type: "bigint"}, {Name: "user_id", Type: "bigint"}},
			ForeignKeys: []ForeignKey{{Name: "orders_user_id_fkey", Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{"id"}}},
		}, {
			Name:       "users",
			Columns:    []Column{{Name: "id", Type: "bigint"}},
			PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 2)
		assert.Contains(t, out.Plan.Statements[0].DDL, "CREATE TABLE users")
		assert.Contains(t, out.Plan.Statements[1].DDL, "CREATE TABLE orders")
		assert.Contains(t, out.Plan.Statements[1].DDL, "FOREIGN KEY (user_id) REFERENCES users (id)")
	})

	t.Run("foreign key cycle between new tables is deferred", func(t *testing.T) {
		desired := Schema{Tables: []Table{{
			Name:        "a",
			Columns:     []Column{{Name: "id", Type: "bigint"}, {Name: "b_id", Type: "bigint"}},
			PrimaryKey:  &PrimaryKey{Name: "a_pkey", Columns: []string{"id"}},
			ForeignKeys: []ForeignKey{{Name: "a_b_id_fkey", Columns: []string{"b_id"}, RefTable: "b", RefColumns: []string{"id"}}},
		}, {
			Name:        "b",
			Columns:     []Column{{Name: "id", Type: "bigint"}, {Name: "a_id", Type: "bigint"}},
			PrimaryKey:  &PrimaryKey{Name: "b_pkey", Columns: []string{"id"}},
			ForeignKeys: []ForeignKey{{Name: "b_a_id_fkey", Columns: []string{"a_id"}, RefTable: "a", RefColumns: []string{"id"}}},
		}}}

		out, err := Diff(context.Background(), DiffInput{Desired: desired})
		require.NoError(t, err)
		var ddls []string
		for _, s := range out.Plan.Statements {
			ddls = append(ddls, s.DDL)
			if strings.HasPrefix(s.DDL, "ALTER TABLE ASYNC") {
				assert.Empty(t, s.Hazards, "new tables have no rows to validate")
			}
		}
		require.Len(t, ddls, 4)
		assert.Contains(t, ddls[0], "CREATE TABLE b")
		assert.NotContains(t, ddls[0], "b_a_id_fkey")
		assert.Contains(t, ddls[1], "CREATE TABLE a")
		assert.Contains(t, ddls[1], "a_b_id_fkey")
		assert.Equal(t, "ALTER TABLE b ADD CONSTRAINT b_a_id_fkey FOREIGN KEY (a_id) REFERENCES a (id) NOT VALID", ddls[2])
		assert.Equal(t, "ALTER TABLE ASYNC b VALIDATE CONSTRAINT b_a_id_fkey", ddls[3])
	})

	t.Run("add, drop, and change foreign keys on existing table", func(t *testing.T) {
		base := func(fks ...ForeignKey) Schema {
			return Schema{Tables: []Table{
				{Name: "users", Columns: []Column{{Name: "id", Type: "bigint"}}, PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}}},
				{Name: "orders", Columns: []Column{{Name: "id", Type: "bigint"}, {Name: "user_id", Type: "bigint"}}, ForeignKeys: fks},
			}}
		}
		fk := ForeignKey{Name: "orders_user_id_fkey", Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{"id"}}
		cascade := fk
		cascade.OnDelete = "CASCADE"

		out, err := Diff(context.Background(), DiffInput{Current: base(), Desired: base(fk)})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 2)
		assert.Equal(t, "ALTER TABLE orders ADD CONSTRAINT orders_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) NOT VALID", out.Plan.Statements[0].DDL)
		assert.Equal(t, "ALTER TABLE ASYNC orders VALIDATE CONSTRAINT orders_user_id_fkey", out.Plan.Statements[1].DDL)
		assert.Equal(t, HazardConstraintValidation, out.Plan.Statements[1].Hazards[0].Type)

		out, err = Diff(context.Background(), DiffInput{Current: base(fk), Desired: base()})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Equal(t, "ALTER TABLE orders DROP CONSTRAINT orders_user_id_fkey", out.Plan.Statements[0].DDL)

		out, err = Diff(context.Background(), DiffInput{Current: base(fk), Desired: base(cascade)})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 3)
		assert.Equal(t, "ALTER TABLE orders DROP CONSTRAINT orders_user_id_fkey", out.Plan.Statements[0].DDL)
		assert.Contains(t, out.Plan.Statements[1].DDL, "ON DELETE CASCADE NOT VALID")

		out, err = Diff(context.Background(), DiffInput{Current: base(fk), Desired: base(fk)})
		require.NoError(t, err)
		assert.Empty(t, out.Plan.Statements)
	})

	t.Run("drop referencing table before referenced table", func(t *testing.T) {
		current := Schema{Tables: []Table{
			{Name: "users", Columns: []Column{{Name: "id", Type: "bigint"}}},
			{Name: "orders", Columns: []Column{{Name: "user_id", Type: "bigint"}},
				ForeignKeys: []ForeignKey{{Name: "orders_user_id_fkey", Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{"id"}}}},
		}}

		out, err := Diff(context.Background(), DiffInput{Current: current})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 2)
		assert.Equal(t, "DROP TABLE orders", out.Plan.Statements[0].DDL)
		assert.Equal(t, "DROP TABLE users", out.Plan.Statements[1].DDL)
	})

	t.Run("expression and partial indexes compare against introspected form", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "u", Columns: []Column{{Name: "email", Type: "text"}}, Indexes: []Index{
			{Name: "idx_lower", Columns: []string{"lower(email)"}},
			{Name: "idx_active", Columns: []string{"email"}, Where: "(deleted_at IS NULL)"},
			{Name: "idx_nulls", Columns: []string{"email NULLS FIRST"}},
		}}}}
		desired := Schema{Tables: []Table{{Name: "u", Columns: []Column{{Name: "email", Type: "text"}}, Indexes: []Index{
			{Name: "idx_lower", Columns: []string{"(lower(email))"}},
			{Name: "idx_active", Columns: []string{"email ASC NULLS LAST"}, Where: "deleted_at IS NULL"},
			{Name: "idx_nulls", Columns: []string{"email nulls first"}},
		}}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		assert.Empty(t, out.Plan.Statements)

		desired.Tables[0].Indexes[1].Where = "deleted_at IS NOT NULL"
		out, err = Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 2)
		assert.Equal(t, "DROP INDEX idx_active", out.Plan.Statements[0].DDL)
		assert.Equal(t, "CREATE INDEX ASYNC idx_active ON u (email ASC NULLS LAST) WHERE deleted_at IS NOT NULL", out.Plan.Statements[1].DDL)
	})

	t.Run("statistics create, drop, replace", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "a", Type: "int"}}, Statistics: []Statistics{
			{Name: "s_keep", Expressions: []string{"a", "b"}},
			{Name: "s_change", Kinds: []string{"ndistinct"}, Expressions: []string{"a", "b"}},
			{Name: "s_old", Expressions: []string{"a", "b"}},
		}}}}
		desired := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "a", Type: "int"}}, Statistics: []Statistics{
			{Name: "s_keep", Kinds: []string{"dependencies", "mcv", "ndistinct"}, Expressions: []string{"a", "b"}},
			{Name: "s_change", Kinds: []string{"mcv"}, Expressions: []string{"a", "b"}},
			{Name: "s_new", Expressions: []string{"(lower(c))"}},
		}}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		var ddls []string
		for _, s := range out.Plan.Statements {
			ddls = append(ddls, s.DDL)
		}
		assert.Equal(t, []string{
			"DROP STATISTICS s_change",
			"DROP STATISTICS s_old",
			"CREATE STATISTICS s_change (mcv) ON a, b FROM t",
			"CREATE STATISTICS s_new ON (lower(c)) FROM t",
		}, ddls)
	})

	t.Run("sequences create, alter, drop", func(t *testing.T) {
		seq := func(name string, cache int64) Sequence {
			return Sequence{Name: name, Increment: 1, MinValue: 1, MaxValue: math.MaxInt64, Start: 1, Cache: cache}
		}
		owned := seq("s_new", 65536)
		owned.OwnedBy = "t.id"
		current := Schema{Sequences: []Sequence{seq("s_same", 1), seq("s_alter", 1), seq("s_old", 1)}}
		desired := Schema{Sequences: []Sequence{seq("s_same", 1), seq("s_alter", 65536), owned}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		var ddls []string
		for _, s := range out.Plan.Statements {
			ddls = append(ddls, s.DDL)
		}
		assert.Equal(t, []string{
			"CREATE SEQUENCE s_new CACHE 65536",
			"ALTER SEQUENCE s_alter CACHE 65536",
			"ALTER SEQUENCE s_new OWNED BY t.id",
			"DROP SEQUENCE IF EXISTS s_old",
		}, ddls)
		assert.Equal(t, HazardDeletesData, out.Plan.Statements[3].Hazards[0].Type)
	})

	t.Run("phase ordering across object types", func(t *testing.T) {
		current := Schema{Tables: []Table{{
			Name:             "t",
			Columns:          []Column{{Name: "id", Type: "bigint"}, {Name: "old", Type: "text", IsNullable: true}},
			CheckConstraints: []CheckConstraint{{Name: "t_old_check", Expression: "(old <> '')"}},
			Indexes:          []Index{{Name: "idx_old", Columns: []string{"old"}}},
		}}}
		desired := Schema{
			Sequences: []Sequence{{Name: "s", Increment: 1, MinValue: 1, MaxValue: math.MaxInt64, Start: 1, Cache: 1}},
			Tables: []Table{{
				Name:             "t",
				Columns:          []Column{{Name: "id", Type: "bigint"}, {Name: "n", Type: "bigint", IsNullable: true, Default: "nextval('s')"}},
				CheckConstraints: []CheckConstraint{{Name: "t_n_check", Expression: "(n > 0)"}},
				Indexes:          []Index{{Name: "idx_n", Columns: []string{"n"}}},
			}},
		}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		var ddls []string
		for _, s := range out.Plan.Statements {
			ddls = append(ddls, s.DDL)
		}
		assert.Equal(t, []string{
			"DROP INDEX idx_old",
			"ALTER TABLE t DROP CONSTRAINT t_old_check",
			"ALTER TABLE t DROP COLUMN old",
			"CREATE SEQUENCE s CACHE 1",
			"ALTER TABLE t ADD COLUMN n BIGINT DEFAULT nextval('s')",
			"CREATE INDEX ASYNC idx_n ON t (n)",
			"ALTER TABLE t ADD CONSTRAINT t_n_check CHECK (n > 0) NOT VALID",
			"ALTER TABLE ASYNC t VALIDATE CONSTRAINT t_n_check",
		}, ddls)
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

	t.Run("add identity drops existing default first", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "id", Type: "bigint", Default: "0"}}}}}
		desired := Schema{Tables: []Table{{Name: "t", Columns: []Column{
			{Name: "id", Type: "bigint", IsIdentity: true, IdentityGeneration: "ALWAYS", IdentityOptions: "CACHE 1"},
		}}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 2)
		assert.Equal(t, "ALTER TABLE t ALTER COLUMN id DROP DEFAULT", out.Plan.Statements[0].DDL)
		assert.Equal(t, "ALTER TABLE t ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (CACHE 1)", out.Plan.Statements[1].DDL)
	})

	t.Run("drop identity is a correctness hazard", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "id", Type: "bigint", IsIdentity: true, IdentityGeneration: "ALWAYS"}}}}}
		desired := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "id", Type: "bigint"}}}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Equal(t, HazardCorrectness, out.Plan.Statements[0].Hazards[0].Type)
	})

	t.Run("identity and generated columns ignore defaults", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "t", Columns: []Column{
			{Name: "id", Type: "bigint", IsIdentity: true, IdentityGeneration: "ALWAYS"},
			{Name: "g", Type: "integer", IsNullable: true, Generated: "((a)::integer * 2)"},
		}}}}
		desired := Schema{Tables: []Table{{Name: "t", Columns: []Column{
			{Name: "id", Type: "bigint", IsIdentity: true, IdentityGeneration: "ALWAYS", IdentityOptions: "CACHE 65536"},
			{Name: "g", Type: "integer", IsNullable: true, Generated: "(a * 2)"},
		}}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		assert.Empty(t, out.Plan.Statements)
	})

	t.Run("changing generation expression returns error", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "g", Type: "integer", Generated: "(a * 2)"}}}}}
		desired := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "g", Type: "integer", Generated: "(a * 3)"}}}}}

		_, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "changing a generation expression")
	})

	t.Run("unchanged unique constraint with NULLS NOT DISTINCT is not a diff", func(t *testing.T) {
		s := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "a", Type: "text"}},
			UniqueConstraints: []UniqueConstraint{{Name: "t_a_key", Columns: []string{"a"}, NullsNotDistinct: true}}}}}
		out, err := Diff(context.Background(), DiffInput{Current: s, Desired: s})
		require.NoError(t, err)
		assert.Empty(t, out.Plan.Statements)
	})

	t.Run("NULLS NOT DISTINCT change replaces the unique constraint", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "a", Type: "text"}},
			UniqueConstraints: []UniqueConstraint{{Name: "t_a_key", Columns: []string{"a"}}}}}}
		desired := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "a", Type: "text"}},
			UniqueConstraints: []UniqueConstraint{{Name: "t_a_key", Columns: []string{"a"}, NullsNotDistinct: true}}}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		var ddls []string
		for _, s := range out.Plan.Statements {
			ddls = append(ddls, s.DDL)
		}
		assert.Equal(t, []string{
			"ALTER TABLE t DROP CONSTRAINT t_a_key",
			"CREATE UNIQUE INDEX ASYNC t_a_key ON t (a) NULLS NOT DISTINCT",
			"ALTER TABLE t ADD CONSTRAINT t_a_key UNIQUE USING INDEX t_a_key",
		}, ddls)
	})

	t.Run("not-valid foreign key is validated", func(t *testing.T) {
		fk := ForeignKey{Name: "o_fk", Columns: []string{"u"}, RefTable: "users", RefColumns: []string{"id"}}
		live := fk
		live.NotValid = true
		current := Schema{Tables: []Table{{Name: "o", Columns: []Column{{Name: "u", Type: "bigint"}}, ForeignKeys: []ForeignKey{live}}}}
		desired := Schema{Tables: []Table{{Name: "o", Columns: []Column{{Name: "u", Type: "bigint"}}, ForeignKeys: []ForeignKey{fk}}}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Equal(t, "ALTER TABLE ASYNC o VALIDATE CONSTRAINT o_fk", out.Plan.Statements[0].DDL)
		assert.Equal(t, HazardConstraintValidation, out.Plan.Statements[0].Hazards[0].Type)
	})

	t.Run("foreign key name difference alone is not a diff", func(t *testing.T) {
		mk := func(name string) Schema {
			return Schema{Tables: []Table{{Name: "o", Columns: []Column{{Name: "u", Type: "bigint"}},
				ForeignKeys: []ForeignKey{{Name: name, Columns: []string{"u"}, RefTable: "users", RefColumns: []string{"id"}, OnDelete: "cascade"}}}}}
		}
		current := mk("o_u_fkey")
		desired := mk("custom_name")
		desired.Tables[0].ForeignKeys[0].OnDelete = "CASCADE"

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		assert.Empty(t, out.Plan.Statements)
	})

	t.Run("foreign key from existing table to new table is added after create", func(t *testing.T) {
		current := Schema{Tables: []Table{{Name: "o", Columns: []Column{{Name: "u", Type: "bigint"}}}}}
		desired := Schema{Tables: []Table{
			{Name: "o", Columns: []Column{{Name: "u", Type: "bigint"}},
				ForeignKeys: []ForeignKey{{Name: "o_u_fkey", Columns: []string{"u"}, RefTable: "users", RefColumns: []string{"id"}}}},
			{Name: "users", Columns: []Column{{Name: "id", Type: "bigint"}}, PrimaryKey: &PrimaryKey{Name: "users_pkey", Columns: []string{"id"}}},
		}}

		out, err := Diff(context.Background(), DiffInput{Current: current, Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 3)
		assert.Contains(t, out.Plan.Statements[0].DDL, "CREATE TABLE users")
		assert.Contains(t, out.Plan.Statements[1].DDL, "ADD CONSTRAINT o_u_fkey FOREIGN KEY")
		assert.Contains(t, out.Plan.Statements[2].DDL, "VALIDATE CONSTRAINT o_u_fkey")
	})

	t.Run("foreign key cycle between dropped tables drops the constraint first", func(t *testing.T) {
		current := Schema{Tables: []Table{
			{Name: "a", Columns: []Column{{Name: "b_id", Type: "bigint"}},
				ForeignKeys: []ForeignKey{{Name: "a_b_fkey", Columns: []string{"b_id"}, RefTable: "b", RefColumns: []string{"id"}}}},
			{Name: "b", Columns: []Column{{Name: "a_id", Type: "bigint"}},
				ForeignKeys: []ForeignKey{{Name: "b_a_fkey", Columns: []string{"a_id"}, RefTable: "a", RefColumns: []string{"id"}}}},
		}}

		out, err := Diff(context.Background(), DiffInput{Current: current})
		require.NoError(t, err)
		var ddls []string
		for _, s := range out.Plan.Statements {
			ddls = append(ddls, s.DDL)
		}
		assert.Equal(t, []string{
			"ALTER TABLE b DROP CONSTRAINT b_a_fkey",
			"DROP TABLE a",
			"DROP TABLE b",
		}, ddls)
	})

	t.Run("self-referencing new table keeps its foreign key inline", func(t *testing.T) {
		desired := Schema{Tables: []Table{{Name: "n", Columns: []Column{{Name: "id", Type: "bigint"}, {Name: "parent", Type: "bigint"}},
			PrimaryKey:  &PrimaryKey{Name: "n_pkey", Columns: []string{"id"}},
			ForeignKeys: []ForeignKey{{Name: "n_parent_fkey", Columns: []string{"parent"}, RefTable: "n", RefColumns: []string{"id"}}}}}}

		out, err := Diff(context.Background(), DiffInput{Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Contains(t, out.Plan.Statements[0].DDL, "FOREIGN KEY (parent) REFERENCES n (id)")
	})

	t.Run("new table statistics and indexes are created", func(t *testing.T) {
		desired := Schema{Tables: []Table{{Name: "t", Columns: []Column{{Name: "a", Type: "int"}, {Name: "b", Type: "int"}},
			Indexes:    []Index{{Name: "idx_a", Columns: []string{"a"}, Where: "a > 0"}},
			Statistics: []Statistics{{Name: "s", Kinds: []string{"ndistinct"}, Expressions: []string{"a", "b"}}}}}}

		out, err := Diff(context.Background(), DiffInput{Desired: desired})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 3)
		assert.Equal(t, "CREATE INDEX ASYNC idx_a ON t (a) WHERE a > 0", out.Plan.Statements[1].DDL)
		assert.Equal(t, "CREATE STATISTICS s (ndistinct) ON a, b FROM t", out.Plan.Statements[2].DDL)
	})

	t.Run("sequence equal to live sequence is not a diff", func(t *testing.T) {
		s := Schema{Sequences: []Sequence{{Name: "s", Increment: -1, MinValue: math.MinInt64, MaxValue: -1, Start: -1, Cache: 65536}}}
		out, err := Diff(context.Background(), DiffInput{Current: s, Desired: s})
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
