package schema

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConstraintDefColumns(t *testing.T) {
	t.Run("primary key", func(t *testing.T) {
		cols := parseConstraintDefColumns("PRIMARY KEY (id)")
		assert.Equal(t, []string{"id"}, cols)
	})

	t.Run("composite unique", func(t *testing.T) {
		cols := parseConstraintDefColumns("UNIQUE (email, tenant_id)")
		assert.Equal(t, []string{"email", "tenant_id"}, cols)
	})

	t.Run("with INCLUDE clause", func(t *testing.T) {
		cols := parseConstraintDefColumns("PRIMARY KEY (id) INCLUDE (name, email)")
		assert.Equal(t, []string{"id"}, cols)
	})

	t.Run("no parens returns nil", func(t *testing.T) {
		cols := parseConstraintDefColumns("INVALID")
		assert.Nil(t, cols)
	})
}

func TestExtractCheckExpression(t *testing.T) {
	t.Run("standard check", func(t *testing.T) {
		expr := extractCheckExpression("CHECK ((x > 0))")
		assert.Equal(t, "((x > 0))", expr)
	})

	t.Run("lowercase check", func(t *testing.T) {
		expr := extractCheckExpression("check ((x > 0))")
		assert.Equal(t, "((x > 0))", expr)
	})

	t.Run("no CHECK prefix", func(t *testing.T) {
		expr := extractCheckExpression("(x > 0)")
		assert.Equal(t, "(x > 0)", expr)
	})
}

func TestIntrospect(t *testing.T) {
	t.Run("validation error on nil DB", func(t *testing.T) {
		_, err := Introspect(context.Background(), IntrospectInput{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid introspect input")
	})

	t.Run("empty database", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("SELECT c.relname").
			WillReturnRows(sqlmock.NewRows([]string{"relname"}))

		out, err := Introspect(context.Background(), IntrospectInput{DB: db})
		require.NoError(t, err)
		assert.Empty(t, out.Schema.Tables)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("single table with columns and constraints", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		// listTables
		mock.ExpectQuery("SELECT c.relname").
			WillReturnRows(sqlmock.NewRows([]string{"relname"}).AddRow("users"))

		// introspectColumns
		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "is_nullable", "default", "is_identity", "identity_char", "attnum"}).
				AddRow("id", "text", false, "", false, "", 1).
				AddRow("email", "text", false, "", false, "", 2))

		// introspectConstraints
		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}).
				AddRow("users_pkey", "p", "PRIMARY KEY (id)").
				AddRow("users_email_key", "u", "UNIQUE (email)"))

		// introspectIndexes
		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "indisunique"}))

		out, err := Introspect(context.Background(), IntrospectInput{DB: db})
		require.NoError(t, err)
		require.Len(t, out.Schema.Tables, 1)

		tbl := out.Schema.Tables[0]
		assert.Equal(t, "users", tbl.Name)
		assert.Len(t, tbl.Columns, 2)
		require.NotNil(t, tbl.PrimaryKey)
		assert.Equal(t, []string{"id"}, tbl.PrimaryKey.Columns)
		require.Len(t, tbl.UniqueConstraints, 1)
		assert.Equal(t, []string{"email"}, tbl.UniqueConstraints[0].Columns)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("table with check constraint", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("SELECT c.relname").
			WillReturnRows(sqlmock.NewRows([]string{"relname"}).AddRow("items"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "is_nullable", "default", "is_identity", "identity_char", "attnum"}).
				AddRow("status", "text", false, "'open'::text", false, "", 1))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}).
				AddRow("items_status_check", "c", "CHECK ((status = ANY (ARRAY['open'::text, 'done'::text])))"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "indisunique"}))

		out, err := Introspect(context.Background(), IntrospectInput{DB: db})
		require.NoError(t, err)
		require.Len(t, out.Schema.Tables[0].CheckConstraints, 1)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("table with indexes", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("SELECT c.relname").
			WillReturnRows(sqlmock.NewRows([]string{"relname"}).AddRow("orders"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "is_nullable", "default", "is_identity", "identity_char", "attnum"}).
				AddRow("id", "text", false, "", false, "", 1).
				AddRow("status", "text", false, "", false, "", 2))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}).
				AddRow("orders_pkey", "p", "PRIMARY KEY (id)"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "indisunique"}).
				AddRow("idx_orders_status", false))

		// introspectIndexColumns
		mock.ExpectQuery("SELECT a.attname").
			WillReturnRows(sqlmock.NewRows([]string{"attname"}).AddRow("status"))

		out, err := Introspect(context.Background(), IntrospectInput{DB: db})
		require.NoError(t, err)
		require.Len(t, out.Schema.Tables[0].Indexes, 1)
		assert.Equal(t, "idx_orders_status", out.Schema.Tables[0].Indexes[0].Name)
		assert.Equal(t, []string{"status"}, out.Schema.Tables[0].Indexes[0].Columns)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("identity column always", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("SELECT c.relname").
			WillReturnRows(sqlmock.NewRows([]string{"relname"}).AddRow("seq"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "is_nullable", "default", "is_identity", "identity_char", "attnum"}).
				AddRow("id", "integer", false, "nextval('seq_id_seq'::regclass)", true, "a", 1))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}).
				AddRow("seq_pkey", "p", "PRIMARY KEY (id)"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "indisunique"}))

		out, err := Introspect(context.Background(), IntrospectInput{DB: db})
		require.NoError(t, err)

		col := out.Schema.Tables[0].Columns[0]
		assert.True(t, col.IsIdentity)
		assert.Equal(t, "ALWAYS", col.IdentityGeneration)
		assert.Empty(t, col.Default) // identity strips nextval default
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("identity column by default", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("SELECT c.relname").
			WillReturnRows(sqlmock.NewRows([]string{"relname"}).AddRow("seq"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "is_nullable", "default", "is_identity", "identity_char", "attnum"}).
				AddRow("id", "integer", false, "nextval('seq_id_seq'::regclass)", true, "d", 1))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "indisunique"}))

		out, err := Introspect(context.Background(), IntrospectInput{DB: db})
		require.NoError(t, err)

		col := out.Schema.Tables[0].Columns[0]
		assert.True(t, col.IsIdentity)
		assert.Equal(t, "BY DEFAULT", col.IdentityGeneration)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
