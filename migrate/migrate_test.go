package migrate

import (
	"context"
	"testing"

	"github.com/tomodian/deesql/internal/schema"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToInternal(t *testing.T) {
	t.Run("carries WaitForJob and hazards", func(t *testing.T) {
		p := Plan{Statements: []Statement{{
			DDL:        "CREATE UNIQUE INDEX ASYNC u ON t (a)",
			Action:     ActionCreate,
			Resource:   "constraint.t.u",
			Hazards:    []Hazard{{Type: HazardConstraintValidation, Message: "m"}},
			WaitForJob: true,
		}}}

		got := toInternal(p)
		require.Len(t, got.Statements, 1)
		assert.Equal(t, schema.Statement{
			DDL:        "CREATE UNIQUE INDEX ASYNC u ON t (a)",
			Action:     schema.ActionCreate,
			Resource:   "constraint.t.u",
			Hazards:    []schema.Hazard{{Type: schema.HazardConstraintValidation, Message: "m"}},
			WaitForJob: true,
		}, got.Statements[0])
	})
}

func TestFromInternal(t *testing.T) {
	t.Run("round trips", func(t *testing.T) {
		p := Plan{Statements: []Statement{
			{DDL: "ALTER TABLE ASYNC t VALIDATE CONSTRAINT c", Action: ActionUpdate, Resource: "constraint.t.c",
				Hazards: []Hazard{{Type: HazardConstraintValidation, Message: "m"}}},
			{DDL: "CREATE UNIQUE INDEX ASYNC u ON t (a)", Action: ActionCreate, Resource: "constraint.t.u",
				Hazards: []Hazard{}, WaitForJob: true},
		}}
		assert.Equal(t, p, fromInternal(toInternal(p)))
	})

	t.Run("hazard constants match internal ones", func(t *testing.T) {
		assert.Equal(t, schema.HazardConstraintValidation, HazardConstraintValidation)
		assert.Equal(t, schema.HazardCorrectness, HazardCorrectness)
	})
}

func TestCheckHazards(t *testing.T) {
	plan := Plan{Statements: []Statement{{
		DDL:     "ALTER TABLE ASYNC t VALIDATE CONSTRAINT c",
		Hazards: []Hazard{{Type: HazardConstraintValidation, Message: "may fail"}},
	}}}

	t.Run("CONSTRAINT_VALIDATION blocked by default", func(t *testing.T) {
		err := CheckHazards(context.Background(), CheckHazardsInput{Plan: plan})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "CONSTRAINT_VALIDATION")
	})

	t.Run("CONSTRAINT_VALIDATION allowed", func(t *testing.T) {
		err := CheckHazards(context.Background(), CheckHazardsInput{Plan: plan, AllowedHazards: []string{HazardConstraintValidation}})
		require.NoError(t, err)
	})
}

func TestApply(t *testing.T) {
	t.Run("waits for async job through the public API", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("CREATE UNIQUE INDEX ASYNC").
			WillReturnRows(sqlmock.NewRows([]string{"job_id"}).AddRow("job1"))
		mock.ExpectQuery(`SELECT sys\.wait_for_job`).WithArgs("job1").
			WillReturnRows(sqlmock.NewRows([]string{"wait_for_job"}).AddRow(true))
		mock.ExpectExec("UNIQUE USING INDEX").WillReturnResult(sqlmock.NewResult(0, 0))

		err = Apply(context.Background(), ApplyInput{DB: db, Plan: Plan{Statements: []Statement{
			{DDL: "CREATE UNIQUE INDEX ASYNC u ON t (a)", WaitForJob: true},
			{DDL: "ALTER TABLE t ADD CONSTRAINT u UNIQUE USING INDEX u"},
		}}})
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty plan is a no-op", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		require.NoError(t, Apply(context.Background(), ApplyInput{DB: db}))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("validation error on nil DB", func(t *testing.T) {
		err := Apply(context.Background(), ApplyInput{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid apply input")
	})
}
