package runner

import (
	"context"
	"fmt"
	"testing"

	"github.com/tomodian/deesql/internal/schema"

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

	t.Run("waits for async job when requested", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("CREATE UNIQUE INDEX ASYNC").
			WillReturnRows(sqlmock.NewRows([]string{"job_id"}).AddRow("jh2gbtx4mzhgfkbimtgwn5j45y"))
		mock.ExpectQuery(`SELECT sys\.wait_for_job`).WithArgs("jh2gbtx4mzhgfkbimtgwn5j45y").
			WillReturnRows(sqlmock.NewRows([]string{"wait_for_job"}).AddRow(true))
		mock.ExpectExec("ADD CONSTRAINT").WillReturnResult(sqlmock.NewResult(0, 0))

		plan := schema.Plan{Statements: []schema.Statement{
			{DDL: "CREATE UNIQUE INDEX ASYNC u ON t (a)", WaitForJob: true},
			{DDL: "ALTER TABLE t ADD CONSTRAINT u UNIQUE USING INDEX u"},
		}}

		err = Execute(context.Background(), ExecuteInput{DB: db, Plan: plan})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("failed async job returns error", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("CREATE UNIQUE INDEX ASYNC").
			WillReturnRows(sqlmock.NewRows([]string{"job_id"}).AddRow("job1"))
		mock.ExpectQuery(`SELECT sys\.wait_for_job`).WithArgs("job1").
			WillReturnRows(sqlmock.NewRows([]string{"wait_for_job"}).AddRow(false))

		plan := schema.Plan{Statements: []schema.Statement{
			{DDL: "CREATE UNIQUE INDEX ASYNC u ON t (a)", WaitForJob: true},
		}}

		err = Execute(context.Background(), ExecuteInput{DB: db, Plan: plan})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "async job job1 did not complete successfully")
	})

	t.Run("no job id means nothing to wait for", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("CREATE UNIQUE INDEX ASYNC").WillReturnRows(sqlmock.NewRows([]string{"job_id"}))

		plan := schema.Plan{Statements: []schema.Statement{
			{DDL: "CREATE UNIQUE INDEX ASYNC u ON t (a)", WaitForJob: true},
		}}

		err = Execute(context.Background(), ExecuteInput{DB: db, Plan: plan})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("job-producing statement retried on OCC conflict", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("CREATE UNIQUE INDEX ASYNC").WillReturnError(fmt.Errorf("ERROR: change conflicts (SQLSTATE 40001)"))
		mock.ExpectQuery("CREATE UNIQUE INDEX ASYNC").
			WillReturnRows(sqlmock.NewRows([]string{"job_id"}).AddRow("job2"))
		mock.ExpectQuery(`SELECT sys\.wait_for_job`).WithArgs("job2").
			WillReturnRows(sqlmock.NewRows([]string{"wait_for_job"}).AddRow(true))

		err = Execute(context.Background(), ExecuteInput{
			DB:         db,
			Plan:       schema.Plan{Statements: []schema.Statement{{DDL: "CREATE UNIQUE INDEX ASYNC u ON t (a)", WaitForJob: true}}},
			MaxRetries: 1,
		})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("wait_for_job query error is returned", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("CREATE UNIQUE INDEX ASYNC").
			WillReturnRows(sqlmock.NewRows([]string{"job_id"}).AddRow("job3"))
		mock.ExpectQuery(`SELECT sys\.wait_for_job`).WillReturnError(fmt.Errorf("function sys.wait_for_job does not exist"))

		err = Execute(context.Background(), ExecuteInput{DB: db, Plan: schema.Plan{Statements: []schema.Statement{
			{DDL: "CREATE UNIQUE INDEX ASYNC u ON t (a)", WaitForJob: true},
		}}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "waiting for job job3")
	})

	t.Run("job-producing statement failure is returned", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("CREATE UNIQUE INDEX ASYNC").WillReturnError(fmt.Errorf("relation exists"))

		err = Execute(context.Background(), ExecuteInput{DB: db, Plan: schema.Plan{Statements: []schema.Statement{
			{DDL: "CREATE UNIQUE INDEX ASYNC u ON t (a)", WaitForJob: true},
		}}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "relation exists")
	})

	t.Run("unreadable job_id is returned", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("CREATE UNIQUE INDEX ASYNC").
			WillReturnRows(sqlmock.NewRows([]string{"job_id", "extra"}).AddRow("job4", "x"))

		err = Execute(context.Background(), ExecuteInput{DB: db, Plan: schema.Plan{Statements: []schema.Statement{
			{DDL: "CREATE UNIQUE INDEX ASYNC u ON t (a)", WaitForJob: true},
		}}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading job_id")
	})
}
