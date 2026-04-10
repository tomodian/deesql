package runner

import (
	"context"
	"fmt"
	"testing"

	"tomodian/deesql/internal/schema"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckHazards(t *testing.T) {
	t.Run("no hazards passes", func(t *testing.T) {
		plan := schema.Plan{
			Statements: []schema.Statement{
				{DDL: "CREATE TABLE t (id TEXT)"},
			},
		}

		err := CheckHazards(context.Background(), CheckHazardsInput{Plan: plan})
		require.NoError(t, err)
	})

	t.Run("blocked hazard returns error", func(t *testing.T) {
		plan := schema.Plan{
			Statements: []schema.Statement{
				{
					DDL:     "DROP TABLE users",
					Hazards: []schema.Hazard{{Type: schema.HazardDeletesData, Message: "drops data"}},
				},
			},
		}

		err := CheckHazards(context.Background(), CheckHazardsInput{Plan: plan})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "DELETES_DATA")
		assert.Contains(t, err.Error(), "--allow-hazards")
	})

	t.Run("allowed hazard passes", func(t *testing.T) {
		plan := schema.Plan{
			Statements: []schema.Statement{
				{
					DDL:     "DROP TABLE users",
					Hazards: []schema.Hazard{{Type: schema.HazardDeletesData, Message: "drops data"}},
				},
			},
		}

		err := CheckHazards(context.Background(), CheckHazardsInput{
			Plan:           plan,
			AllowedHazards: []string{schema.HazardDeletesData},
		})
		require.NoError(t, err)
	})

	t.Run("multiple hazards partially allowed", func(t *testing.T) {
		plan := schema.Plan{
			Statements: []schema.Statement{
				{
					DDL: "DROP TABLE users",
					Hazards: []schema.Hazard{
						{Type: schema.HazardDeletesData, Message: "drops data"},
						{Type: schema.HazardIndexDropped, Message: "drops index"},
					},
				},
			},
		}

		err := CheckHazards(context.Background(), CheckHazardsInput{
			Plan:           plan,
			AllowedHazards: []string{schema.HazardDeletesData},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "INDEX_DROPPED")
	})

	t.Run("all hazards allowed", func(t *testing.T) {
		plan := schema.Plan{
			Statements: []schema.Statement{
				{
					DDL: "complex migration",
					Hazards: []schema.Hazard{
						{Type: schema.HazardDeletesData, Message: "drops data"},
						{Type: schema.HazardIndexBuild, Message: "builds index"},
						{Type: schema.HazardIndexDropped, Message: "drops index"},
					},
				},
			},
		}

		err := CheckHazards(context.Background(), CheckHazardsInput{
			Plan:           plan,
			AllowedHazards: []string{schema.HazardDeletesData, schema.HazardIndexBuild, schema.HazardIndexDropped},
		})
		require.NoError(t, err)
	})

	t.Run("empty plan passes", func(t *testing.T) {
		err := CheckHazards(context.Background(), CheckHazardsInput{Plan: schema.Plan{}})
		require.NoError(t, err)
	})
}

func TestExecute(t *testing.T) {
	t.Run("executes all statements", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectExec("CREATE TABLE t").WillReturnResult(sqlmock.NewResult(0, 0))

		plan := schema.Plan{
			Statements: []schema.Statement{
				{DDL: "CREATE TABLE t (id TEXT)"},
			},
		}

		err = Execute(context.Background(), ExecuteInput{DB: db, Plan: plan})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("returns error on statement failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectExec("CREATE TABLE").WillReturnError(fmt.Errorf("table exists"))

		plan := schema.Plan{
			Statements: []schema.Statement{
				{DDL: "CREATE TABLE t (id TEXT)"},
			},
		}

		err = Execute(context.Background(), ExecuteInput{DB: db, Plan: plan})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "statement 1 failed")
	})

	t.Run("validation error on nil DB", func(t *testing.T) {
		err := Execute(context.Background(), ExecuteInput{Plan: schema.Plan{}})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid execute input")
	})

	t.Run("multiple statements executed in order", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectExec("CREATE TABLE a").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("CREATE TABLE b").WillReturnResult(sqlmock.NewResult(0, 0))

		plan := schema.Plan{
			Statements: []schema.Statement{
				{DDL: "CREATE TABLE a (id TEXT)"},
				{DDL: "CREATE TABLE b (id TEXT)"},
			},
		}

		err = Execute(context.Background(), ExecuteInput{DB: db, Plan: plan})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty plan succeeds", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		err = Execute(context.Background(), ExecuteInput{DB: db, Plan: schema.Plan{}})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
