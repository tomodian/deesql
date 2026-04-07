package planner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePlan(t *testing.T) {
	t.Run("validation error on nil DB", func(t *testing.T) {
		_, err := GeneratePlan(context.Background(), GeneratePlanInput{
			SchemaFiles: []string{"/tmp/x.sql"},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid plan input")
	})

	t.Run("validation error on empty schema files", func(t *testing.T) {
		db, _, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		_, err = GeneratePlan(context.Background(), GeneratePlanInput{
			DB:          db,
			SchemaFiles: []string{},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid plan input")
	})

	t.Run("parse error on bad SQL", func(t *testing.T) {
		db, _, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		f := writeTemp(t, "bad.sql", "INSERT INTO foo VALUES (1);")

		_, err = GeneratePlan(context.Background(), GeneratePlanInput{
			DB:          db,
			SchemaFiles: []string{f},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing schema files")
	})

	t.Run("successful plan with empty DB", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		f := writeTemp(t, "schema.sql", `
CREATE TABLE users (
    id   TEXT NOT NULL,
    name TEXT NOT NULL,
    PRIMARY KEY (id)
);
`)
		// Introspect: listTables returns empty
		mock.ExpectQuery("SELECT c.relname").
			WillReturnRows(sqlmock.NewRows([]string{"relname"}))

		out, err := GeneratePlan(context.Background(), GeneratePlanInput{
			DB:          db,
			SchemaFiles: []string{f},
		})
		require.NoError(t, err)
		require.Len(t, out.Plan.Statements, 1)
		assert.Contains(t, out.Plan.Statements[0].DDL, "CREATE TABLE users")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no changes when schema matches", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		f := writeTemp(t, "schema.sql", `
CREATE TABLE users (
    id   TEXT NOT NULL,
    name TEXT NOT NULL,
    PRIMARY KEY (id)
);
`)
		// Introspect: listTables
		mock.ExpectQuery("SELECT c.relname").
			WillReturnRows(sqlmock.NewRows([]string{"relname"}).AddRow("users"))

		// introspectColumns
		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{
				"attname", "format_type", "is_nullable", "default",
				"is_identity", "identity_char", "attnum",
			}).
				AddRow("id", "text", false, "", false, "", 1).
				AddRow("name", "text", false, "", false, "", 2))

		// introspectConstraints
		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}).
				AddRow("users_pkey", "p", "PRIMARY KEY (id)"))

		// introspectIndexes
		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "indisunique"}))

		out, err := GeneratePlan(context.Background(), GeneratePlanInput{
			DB:          db,
			SchemaFiles: []string{f},
		})
		require.NoError(t, err)
		assert.Empty(t, out.Plan.Statements)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("introspect error propagated", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		f := writeTemp(t, "schema.sql", `CREATE TABLE t (id TEXT PRIMARY KEY);`)

		mock.ExpectQuery("SELECT c.relname").
			WillReturnError(assert.AnError)

		_, err = GeneratePlan(context.Background(), GeneratePlanInput{
			DB:          db,
			SchemaFiles: []string{f},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "introspecting live schema")
	})
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	err := os.WriteFile(p, []byte(content), 0644)
	require.NoError(t, err)
	return p
}
