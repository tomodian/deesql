package schema

import (
	"context"
	"math"
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

		// introspectSequences
		mock.ExpectQuery("FROM pg_sequence").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "seqincrement", "seqmin", "seqmax", "seqstart", "seqcache", "seqcycle"}))

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
			WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "is_nullable", "default", "is_identity", "identity_char", "attgenerated", "attnum"}).
				AddRow("id", "text", false, "", false, "", "", 1).
				AddRow("email", "text", false, "", false, "", "", 2))

		// introspectConstraints
		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}).
				AddRow("users_pkey", "p", "PRIMARY KEY (id)").
				AddRow("users_email_key", "u", "UNIQUE (email)"))

		// introspectIndexes
		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "indexdef"}))

		// introspectStatistics
		mock.ExpectQuery("FROM pg_statistic_ext").
			WillReturnRows(sqlmock.NewRows([]string{"stxname", "def"}))

		// introspectSequences
		mock.ExpectQuery("FROM pg_sequence").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "seqincrement", "seqmin", "seqmax", "seqstart", "seqcache", "seqcycle"}))

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
			WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "is_nullable", "default", "is_identity", "identity_char", "attgenerated", "attnum"}).
				AddRow("status", "text", false, "'open'::text", false, "", "", 1))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}).
				AddRow("items_status_check", "c", "CHECK ((status = ANY (ARRAY['open'::text, 'done'::text])))"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "indexdef"}))

		// introspectStatistics
		mock.ExpectQuery("FROM pg_statistic_ext").
			WillReturnRows(sqlmock.NewRows([]string{"stxname", "def"}))

		// introspectSequences
		mock.ExpectQuery("FROM pg_sequence").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "seqincrement", "seqmin", "seqmax", "seqstart", "seqcache", "seqcycle"}))

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
			WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "is_nullable", "default", "is_identity", "identity_char", "attgenerated", "attnum"}).
				AddRow("id", "text", false, "", false, "", "", 1).
				AddRow("status", "text", false, "", false, "", "", 2))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}).
				AddRow("orders_pkey", "p", "PRIMARY KEY (id)"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "indexdef"}).
				AddRow("idx_orders_status", "CREATE INDEX idx_orders_status ON public.orders USING btree_index (status, lower(id) NULLS FIRST) INCLUDE (id) WHERE (status <> 'done'::text)"))

		// introspectStatistics
		mock.ExpectQuery("FROM pg_statistic_ext").
			WillReturnRows(sqlmock.NewRows([]string{"stxname", "def"}))

		// introspectSequences
		mock.ExpectQuery("FROM pg_sequence").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "seqincrement", "seqmin", "seqmax", "seqstart", "seqcache", "seqcycle"}))

		out, err := Introspect(context.Background(), IntrospectInput{DB: db})
		require.NoError(t, err)
		require.Len(t, out.Schema.Tables[0].Indexes, 1)
		assert.Equal(t, "idx_orders_status", out.Schema.Tables[0].Indexes[0].Name)
		assert.Equal(t, []string{"status", "lower(id) NULLS FIRST"}, out.Schema.Tables[0].Indexes[0].Columns)
		assert.Equal(t, []string{"id"}, out.Schema.Tables[0].Indexes[0].IncludeColumns)
		assert.Equal(t, "(status <> 'done'::text)", out.Schema.Tables[0].Indexes[0].Where)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("identity column always", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("SELECT c.relname").
			WillReturnRows(sqlmock.NewRows([]string{"relname"}).AddRow("seq"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "is_nullable", "default", "is_identity", "identity_char", "attgenerated", "attnum"}).
				AddRow("id", "integer", false, "nextval('seq_id_seq'::regclass)", true, "a", "", 1))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}).
				AddRow("seq_pkey", "p", "PRIMARY KEY (id)"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "indexdef"}))

		// introspectStatistics
		mock.ExpectQuery("FROM pg_statistic_ext").
			WillReturnRows(sqlmock.NewRows([]string{"stxname", "def"}))

		// introspectSequences
		mock.ExpectQuery("FROM pg_sequence").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "seqincrement", "seqmin", "seqmax", "seqstart", "seqcache", "seqcycle"}))

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
			WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "is_nullable", "default", "is_identity", "identity_char", "attgenerated", "attnum"}).
				AddRow("id", "integer", false, "nextval('seq_id_seq'::regclass)", true, "d", "", 1))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "indexdef"}))

		// introspectStatistics
		mock.ExpectQuery("FROM pg_statistic_ext").
			WillReturnRows(sqlmock.NewRows([]string{"stxname", "def"}))

		// introspectSequences
		mock.ExpectQuery("FROM pg_sequence").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "seqincrement", "seqmin", "seqmax", "seqstart", "seqcache", "seqcycle"}))

		out, err := Introspect(context.Background(), IntrospectInput{DB: db})
		require.NoError(t, err)

		col := out.Schema.Tables[0].Columns[0]
		assert.True(t, col.IsIdentity)
		assert.Equal(t, "BY DEFAULT", col.IdentityGeneration)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("foreign keys, NOT VALID, generated columns, statistics, sequences", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()

		mock.ExpectQuery("SELECT c.relname").
			WillReturnRows(sqlmock.NewRows([]string{"relname"}).AddRow("orders"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "is_nullable", "default", "is_identity", "identity_char", "attgenerated", "attnum"}).
				AddRow("id", "bigint", false, "", false, "", "", 1).
				AddRow("total", "integer", true, "(qty * 2)", false, "", "s", 2))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}).
				AddRow("orders_qty_check", "c", "CHECK ((qty > 0)) NOT VALID").
				AddRow("orders_user_id_fkey", "f", "FOREIGN KEY (user_id) REFERENCES users(id) ON UPDATE CASCADE ON DELETE SET NULL DEFERRABLE INITIALLY DEFERRED NOT VALID").
				AddRow("orders_tenant_key", "u", "UNIQUE NULLS NOT DISTINCT (tenant_id)"))

		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "indexdef"}))

		mock.ExpectQuery("FROM pg_statistic_ext").
			WillReturnRows(sqlmock.NewRows([]string{"stxname", "def"}).
				AddRow("s_ab", "CREATE STATISTICS public.s_ab ON a, b FROM public.orders").
				AddRow("s_nd", "CREATE STATISTICS public.s_nd (ndistinct) ON a, b FROM public.orders"))

		mock.ExpectQuery("FROM pg_sequence").
			WillReturnRows(sqlmock.NewRows([]string{"relname", "seqincrement", "seqmin", "seqmax", "seqstart", "seqcache", "seqcycle"}).
				AddRow("order_seq", 1, 1, int64(math.MaxInt64), 1, 65536, false))

		out, err := Introspect(context.Background(), IntrospectInput{DB: db})
		require.NoError(t, err)
		tbl := out.Schema.Tables[0]

		assert.Equal(t, "(qty * 2)", tbl.Columns[1].Generated)
		assert.Empty(t, tbl.Columns[1].Default)

		require.Len(t, tbl.CheckConstraints, 1)
		assert.True(t, tbl.CheckConstraints[0].NotValid)
		assert.Equal(t, "((qty > 0))", tbl.CheckConstraints[0].Expression)

		require.Len(t, tbl.ForeignKeys, 1)
		assert.Equal(t, ForeignKey{
			Name: "orders_user_id_fkey", Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{"id"},
			OnDelete: "SET NULL", OnUpdate: "CASCADE", Deferrable: true, InitiallyDeferred: true, NotValid: true,
		}, tbl.ForeignKeys[0])

		require.Len(t, tbl.UniqueConstraints, 1)
		assert.True(t, tbl.UniqueConstraints[0].NullsNotDistinct)

		assert.Equal(t, []Statistics{
			{Name: "s_ab", Kinds: []string{"dependencies", "mcv", "ndistinct"}, Expressions: []string{"a", "b"}},
			{Name: "s_nd", Kinds: []string{"ndistinct"}, Expressions: []string{"a", "b"}},
		}, tbl.Statistics)

		assert.Equal(t, []Sequence{{Name: "order_seq", Increment: 1, MinValue: 1, MaxValue: math.MaxInt64, Start: 1, Cache: 65536}}, out.Schema.Sequences)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("errors from new introspection queries are wrapped", func(t *testing.T) {
		setup := func(t *testing.T) (sqlmock.Sqlmock, func() error) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { db.Close() })
			mock.ExpectQuery("SELECT c.relname").WillReturnRows(sqlmock.NewRows([]string{"relname"}).AddRow("t"))
			mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"attname", "format_type", "is_nullable", "default", "is_identity", "identity_char", "attgenerated", "attnum"}))
			return mock, func() error {
				_, err := Introspect(context.Background(), IntrospectInput{DB: db})
				return err
			}
		}
		emptyConstraints := func() *sqlmock.Rows { return sqlmock.NewRows([]string{"conname", "contype", "constraintdef"}) }
		emptyIndexes := func() *sqlmock.Rows { return sqlmock.NewRows([]string{"relname", "indexdef"}) }

		t.Run("bad foreign key definition", func(t *testing.T) {
			mock, run := setup(t)
			mock.ExpectQuery("SELECT").WillReturnRows(emptyConstraints().AddRow("t_fk", "f", "FOREIGN KEY oops"))
			err := run()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "constraint t.t_fk")
		})

		t.Run("bad index definition", func(t *testing.T) {
			mock, run := setup(t)
			mock.ExpectQuery("SELECT").WillReturnRows(emptyConstraints())
			mock.ExpectQuery("SELECT").WillReturnRows(emptyIndexes().AddRow("i", "garbage"))
			err := run()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "parsing definition of index i")
		})

		t.Run("statistics query fails", func(t *testing.T) {
			mock, run := setup(t)
			mock.ExpectQuery("SELECT").WillReturnRows(emptyConstraints())
			mock.ExpectQuery("SELECT").WillReturnRows(emptyIndexes())
			mock.ExpectQuery("FROM pg_statistic_ext").WillReturnError(assert.AnError)
			err := run()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "listing statistics for t")
		})

		t.Run("bad statistics definition", func(t *testing.T) {
			mock, run := setup(t)
			mock.ExpectQuery("SELECT").WillReturnRows(emptyConstraints())
			mock.ExpectQuery("SELECT").WillReturnRows(emptyIndexes())
			mock.ExpectQuery("FROM pg_statistic_ext").WillReturnRows(sqlmock.NewRows([]string{"stxname", "def"}).AddRow("s", "nonsense"))
			err := run()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "parsing definition of statistics s")
		})

		t.Run("sequence query fails", func(t *testing.T) {
			mock, run := setup(t)
			mock.ExpectQuery("SELECT").WillReturnRows(emptyConstraints())
			mock.ExpectQuery("SELECT").WillReturnRows(emptyIndexes())
			mock.ExpectQuery("FROM pg_statistic_ext").WillReturnRows(sqlmock.NewRows([]string{"stxname", "def"}))
			mock.ExpectQuery("FROM pg_sequence").WillReturnError(assert.AnError)
			err := run()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "listing sequences")
		})
	})
}
