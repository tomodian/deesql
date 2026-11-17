package proxy

import (
	"github.com/tomodian/deesql/internal/rules"

	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheck(t *testing.T) {
	t.Run("allowed statements pass through", func(t *testing.T) {
		allowed := []string{
			"SELECT 1",
			"INSERT INTO users (id, name) VALUES ('1', 'alice')",
			"CREATE TABLE t (id TEXT PRIMARY KEY)",
			"CREATE INDEX ASYNC idx ON t (col)",
			"CREATE UNIQUE INDEX ASYNC idx ON t (col)",
			"DELETE FROM users WHERE id = '1'",
			"UPDATE users SET name = 'bob' WHERE id = '1'",
			"DROP TABLE users",
			"BEGIN",
			"COMMIT",
			"ROLLBACK",
			"CREATE FUNCTION add(a int, b int) RETURNS int LANGUAGE SQL AS 'SELECT a + b'",
			// Supported since 2026 Aurora DSQL releases
			"CREATE TABLE o (id bigint PRIMARY KEY, user_id bigint REFERENCES users(id) ON DELETE CASCADE)",
			"CREATE TABLE o (id bigint, FOREIGN KEY (id) REFERENCES other(id))",
			"ALTER TABLE o ADD CONSTRAINT o_fk FOREIGN KEY (user_id) REFERENCES users (id) NOT VALID",
			"ALTER TABLE o ADD CONSTRAINT o_check CHECK (qty > 0) NOT VALID",
			"ALTER TABLE ASYNC o VALIDATE CONSTRAINT o_check",
			"ALTER TABLE o ADD CONSTRAINT o_email_key UNIQUE USING INDEX o_email_key",
			"ALTER TABLE o DROP CONSTRAINT o_check",
			"ALTER TABLE o DROP COLUMN a, DROP COLUMN b",
			"ALTER TABLE o ALTER COLUMN a SET DEFAULT 0",
			"ALTER TABLE o ALTER COLUMN a DROP DEFAULT",
			"ALTER TABLE o ALTER COLUMN a DROP NOT NULL",
			"ALTER TABLE o ALTER COLUMN g DROP EXPRESSION",
			"ALTER TABLE o ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (CACHE 65536)",
			"ALTER TABLE o ALTER COLUMN id DROP IDENTITY",
			"CREATE INDEX ASYNC idx ON t ((lower(email)), created_at NULLS FIRST) WHERE deleted_at IS NULL",
			"CREATE UNIQUE INDEX ASYNC idx ON t (a) INCLUDE (b) NULLS NOT DISTINCT",
			"CREATE STATISTICS s (ndistinct) ON a, b FROM t",
			"CREATE SEQUENCE s CACHE 65536",
			"CREATE SEQUENCE s CACHE 1 START WITH 10",
			"CREATE TABLE t (id bigint GENERATED ALWAYS AS IDENTITY (CACHE 70000) PRIMARY KEY)",
			"CREATE TABLE t (a int, total int GENERATED ALWAYS AS (a * 2) STORED)",
			"CREATE TABLE t (doc jsonb, raw json)",
			"ALTER SYSTEM SET dsql.enable_batched_nestloop = off",
			"ALTER SYSTEM RESET ALL",
			"SET dsql.enable_batched_nestloop = off",
		}
		for _, sql := range allowed {
			assert.Empty(t, Check(sql), "expected %q to be allowed", sql)
		}
	})

	t.Run("CREATE DATABASE blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE DATABASE statements are unsupported", Check("CREATE DATABASE mydb"))
	})

	t.Run("CREATE EXTENSION blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE EXTENSION statements are unsupported", Check("CREATE EXTENSION pgcrypto"))
	})

	t.Run("CREATE TRIGGER blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE TRIGGER statements are unsupported", Check("CREATE TRIGGER trg AFTER INSERT ON t FOR EACH ROW EXECUTE FUNCTION fn()"))
	})

	t.Run("CREATE OR REPLACE TRIGGER blocked", func(t *testing.T) {
		assert.Contains(t, Check("CREATE OR REPLACE TRIGGER trg AFTER INSERT ON t FOR EACH ROW EXECUTE FUNCTION fn()"), "TRIGGER")
	})

	t.Run("CREATE TABLESPACE blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE TABLESPACE statements are unsupported", Check("CREATE TABLESPACE ts LOCATION '/data'"))
	})

	t.Run("CREATE MATERIALIZED VIEW blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE MATERIALIZED VIEW or CREATE TABLE AS statements are unsupported", Check("CREATE MATERIALIZED VIEW mv AS SELECT 1"))
	})

	t.Run("CREATE TABLE AS SELECT blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE MATERIALIZED VIEW or CREATE TABLE AS statements are unsupported", Check("CREATE TABLE t AS SELECT 1"))
	})

	t.Run("CREATE TYPE enum blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE TYPE (enum types) statements are unsupported", Check("CREATE TYPE mood AS ENUM ('happy', 'sad')"))
	})

	t.Run("CREATE TYPE range blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE TYPE (range types) statements are unsupported", Check("CREATE TYPE floatrange AS RANGE (subtype = float8)"))
	})

	t.Run("CREATE TYPE composite blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE TYPE (composite types) statements are unsupported", Check("CREATE TYPE address AS (street text, city text)"))
	})

	t.Run("TRUNCATE blocked", func(t *testing.T) {
		assert.Equal(t, "TRUNCATE TABLE statements are unsupported", Check("TRUNCATE TABLE users"))
	})

	t.Run("VACUUM blocked", func(t *testing.T) {
		assert.Equal(t, "VACUUM statements are unsupported", Check("VACUUM users"))
	})

	t.Run("CREATE TEMPORARY TABLE blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE TEMPORARY TABLE is unsupported", Check("CREATE TEMPORARY TABLE tmp (id int)"))
	})

	t.Run("CREATE TEMP TABLE blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE TEMPORARY TABLE is unsupported", Check("CREATE TEMP TABLE tmp (id int)"))
	})

	t.Run("INHERITS blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE TABLE INHERITS is unsupported", Check("CREATE TABLE child (extra text) INHERITS (parent)"))
	})

	t.Run("PARTITION BY in CREATE TABLE blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE TABLE PARTITION is unsupported", Check("CREATE TABLE t (id int) PARTITION BY RANGE (id)"))
	})

	t.Run("PARTITION BY in window function allowed", func(t *testing.T) {
		assert.Empty(t, Check("SELECT RANK() OVER (PARTITION BY project_id ORDER BY priority) FROM tasks"))
	})

	t.Run("COLLATE blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE TABLE COLLATE is unsupported", Check("CREATE TABLE t (name text COLLATE \"en_US\")"))
	})

	t.Run("CREATE INDEX with ASC blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE Index with ordering ASC or DESC is unsupported", Check("CREATE INDEX idx ON t (col ASC)"))
	})

	t.Run("CREATE INDEX with DESC blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE Index with ordering ASC or DESC is unsupported", Check("CREATE INDEX idx ON t (col DESC)"))
	})

	t.Run("LANGUAGE plpgsql blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE FUNCTION with language plpgsql not supported", Check("CREATE FUNCTION fn() RETURNS void LANGUAGE plpgsql AS $$ BEGIN END $$"))
	})

	t.Run("LANGUAGE plv8 blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE FUNCTION with language plv8 not supported", Check("CREATE FUNCTION fn() RETURNS void LANGUAGE plv8 AS $$ $$"))
	})

	t.Run("CREATE PROCEDURE blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE PROCEDURE statements are unsupported", Check("CREATE PROCEDURE myproc() LANGUAGE SQL AS $$ SELECT 1 $$"))
	})

	t.Run("CREATE OR REPLACE PROCEDURE blocked", func(t *testing.T) {
		assert.Contains(t, Check("CREATE OR REPLACE PROCEDURE myproc() LANGUAGE SQL AS $$ SELECT 1 $$"), "PROCEDURE")
	})

	t.Run("CREATE RULE blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE RULE statements are unsupported", Check("CREATE RULE myrule AS ON INSERT TO t DO NOTHING"))
	})

	t.Run("CREATE UNLOGGED TABLE blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE UNLOGGED TABLE is unsupported", Check("CREATE UNLOGGED TABLE t (id int)"))
	})

	t.Run("non-btree index hash blocked", func(t *testing.T) {
		assert.Equal(t, "only btree indexes supported (hash not supported)", Check("CREATE INDEX ASYNC idx ON t USING hash (col)"))
	})

	t.Run("non-btree index gin blocked", func(t *testing.T) {
		assert.Equal(t, "only btree indexes supported (gin not supported)", Check("CREATE INDEX ASYNC idx ON t USING gin (col)"))
	})

	t.Run("non-btree index gist blocked", func(t *testing.T) {
		assert.Equal(t, "only btree indexes supported (gist not supported)", Check("CREATE INDEX ASYNC idx ON t USING gist (col)"))
	})

	t.Run("non-btree index brin blocked", func(t *testing.T) {
		assert.Equal(t, "only btree indexes supported (brin not supported)", Check("CREATE INDEX ASYNC idx ON t USING brin (col)"))
	})

	t.Run("non-btree index spgist blocked", func(t *testing.T) {
		assert.Equal(t, "only btree indexes supported (spgist not supported)", Check("CREATE INDEX ASYNC idx ON t USING spgist (col)"))
	})

	t.Run("CREATE INDEX CONCURRENTLY blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE INDEX CONCURRENTLY not supported (use CREATE INDEX ASYNC)", Check("CREATE INDEX CONCURRENTLY idx ON t (col)"))
	})

	t.Run("CREATE INDEX without ASYNC blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE INDEX must use ASYNC (use CREATE INDEX ASYNC)", Check("CREATE INDEX idx ON t (col)"))
	})

	t.Run("CREATE UNIQUE INDEX without ASYNC blocked", func(t *testing.T) {
		assert.Equal(t, "CREATE INDEX must use ASYNC (use CREATE INDEX ASYNC)", Check("CREATE UNIQUE INDEX idx ON t (col)"))
	})

	t.Run("ALTER TABLE ADD FOREIGN KEY without NOT VALID blocked", func(t *testing.T) {
		assert.Equal(t, rules.MsgAddConstraintValid, Check("ALTER TABLE t ADD CONSTRAINT fk FOREIGN KEY (id) REFERENCES other(id)"))
		assert.Equal(t, rules.MsgAddConstraintValid, Check("ALTER TABLE t ADD FOREIGN KEY (id) REFERENCES other(id)"))
	})

	t.Run("ALTER TABLE ADD CHECK without NOT VALID blocked", func(t *testing.T) {
		assert.Equal(t, rules.MsgAddConstraintValid, Check("ALTER TABLE t ADD CONSTRAINT c CHECK (x > 0)"))
	})

	t.Run("ALTER TABLE ADD PRIMARY KEY blocked", func(t *testing.T) {
		assert.Equal(t, rules.MsgAddPrimaryKey, Check("ALTER TABLE t ADD PRIMARY KEY (id)"))
	})

	t.Run("ALTER TABLE ADD UNIQUE without USING INDEX blocked", func(t *testing.T) {
		assert.Equal(t, rules.MsgAddUnique, Check("ALTER TABLE t ADD CONSTRAINT u UNIQUE (email)"))
	})

	t.Run("ALTER COLUMN TYPE blocked", func(t *testing.T) {
		assert.Equal(t, rules.MsgAlterColumnType, Check("ALTER TABLE t ALTER COLUMN a TYPE bigint"))
		assert.Equal(t, rules.MsgAlterColumnType, Check("ALTER TABLE t ALTER a SET DATA TYPE bigint"))
	})

	t.Run("SET NOT NULL blocked", func(t *testing.T) {
		assert.Equal(t, rules.MsgSetNotNull, Check("ALTER TABLE t ALTER COLUMN a SET NOT NULL"))
	})

	t.Run("sequence rules", func(t *testing.T) {
		assert.Equal(t, rules.MsgSequenceCache, Check("CREATE SEQUENCE s"))
		assert.Equal(t, rules.MsgCacheRange, Check("CREATE SEQUENCE s CACHE 100"))
		assert.Equal(t, rules.MsgCacheRange, Check("ALTER SEQUENCE s CACHE 65535"))
		assert.Equal(t, rules.MsgSequenceType, Check("CREATE SEQUENCE s AS integer CACHE 1"))
	})

	t.Run("identity rules", func(t *testing.T) {
		assert.Equal(t, rules.MsgIdentityType, Check("CREATE TABLE t (id integer GENERATED ALWAYS AS IDENTITY (CACHE 1))"))
		assert.Equal(t, rules.MsgIdentityCache, Check("CREATE TABLE t (id bigint GENERATED ALWAYS AS IDENTITY)"))
		assert.Equal(t, rules.MsgIdentityCache, Check("ALTER TABLE t ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY"))
	})

	t.Run("CREATE INDEX without ASYNC after a comment blocked", func(t *testing.T) {
		assert.Equal(t, rules.MsgIndexAsync, Check("-- build the index\nCREATE INDEX idx ON t (col)"))
	})

	t.Run("EXCLUDE constraint blocked", func(t *testing.T) {
		assert.Equal(t, "EXCLUDE constraint not supported", Check("CREATE TABLE t (id int, EXCLUDE (id WITH =))"))
	})

	t.Run("SAVEPOINT blocked", func(t *testing.T) {
		assert.Equal(t, "SAVEPOINT statements are unsupported", Check("SAVEPOINT my_savepoint"))
	})

	t.Run("RELEASE SAVEPOINT blocked", func(t *testing.T) {
		assert.Equal(t, "RELEASE SAVEPOINT statements are unsupported", Check("RELEASE SAVEPOINT my_savepoint"))
	})

	t.Run("ROLLBACK TO SAVEPOINT blocked", func(t *testing.T) {
		assert.Contains(t, Check("ROLLBACK TO SAVEPOINT my_savepoint"), "SAVEPOINT")
	})

	t.Run("LISTEN blocked", func(t *testing.T) {
		assert.Equal(t, "LISTEN statements are unsupported", Check("LISTEN my_channel"))
	})

	t.Run("NOTIFY blocked", func(t *testing.T) {
		assert.Equal(t, "NOTIFY statements are unsupported", Check("NOTIFY my_channel, 'payload'"))
	})

	t.Run("UNLISTEN blocked", func(t *testing.T) {
		assert.Equal(t, "UNLISTEN statements are unsupported", Check("UNLISTEN my_channel"))
	})

	t.Run("LOCK TABLE blocked", func(t *testing.T) {
		assert.Equal(t, "LOCK TABLE statements are unsupported", Check("LOCK TABLE users IN EXCLUSIVE MODE"))
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		assert.Equal(t, "CREATE DATABASE statements are unsupported", Check("create database mydb"))
		assert.Equal(t, "VACUUM statements are unsupported", Check("vacuum analyze users"))
	})

	t.Run("multi-statement with one blocked", func(t *testing.T) {
		msg := Check("SELECT 1; CREATE EXTENSION pgcrypto; SELECT 2")
		assert.Equal(t, "CREATE EXTENSION statements are unsupported", msg)
	})

	t.Run("multi-statement all allowed", func(t *testing.T) {
		assert.Empty(t, Check("SELECT 1; SELECT 2; SELECT 3"))
	})

	t.Run("semicolon inside string literal ignored", func(t *testing.T) {
		assert.Empty(t, Check("SELECT 'hello; world'"))
	})

	t.Run("empty string allowed", func(t *testing.T) {
		assert.Empty(t, Check(""))
	})

	t.Run("whitespace only allowed", func(t *testing.T) {
		assert.Empty(t, Check("   ;  ;  "))
	})
}

func TestRewrite(t *testing.T) {
	t.Run("CREATE INDEX ASYNC to CONCURRENTLY", func(t *testing.T) {
		assert.Equal(t, "CREATE INDEX CONCURRENTLY idx ON t (col)", Rewrite("CREATE INDEX ASYNC idx ON t (col)"))
	})

	t.Run("CREATE UNIQUE INDEX ASYNC to CONCURRENTLY", func(t *testing.T) {
		assert.Equal(t, "CREATE UNIQUE INDEX CONCURRENTLY idx ON t (col)", Rewrite("CREATE UNIQUE INDEX ASYNC idx ON t (col)"))
	})

	t.Run("case insensitive", func(t *testing.T) {
		assert.Equal(t, "create index CONCURRENTLY idx ON t (col)", Rewrite("create index async idx ON t (col)"))
	})

	t.Run("non-index SQL unchanged", func(t *testing.T) {
		assert.Equal(t, "SELECT 1", Rewrite("SELECT 1"))
	})

	t.Run("CREATE INDEX without ASYNC unchanged", func(t *testing.T) {
		assert.Equal(t, "CREATE INDEX idx ON t (col)", Rewrite("CREATE INDEX idx ON t (col)"))
	})

	t.Run("ALTER TABLE ASYNC loses ASYNC", func(t *testing.T) {
		assert.Equal(t, "ALTER TABLE t VALIDATE CONSTRAINT c", Rewrite("ALTER TABLE ASYNC t VALIDATE CONSTRAINT c"))
	})
}

func TestCheckSessionParams(t *testing.T) {
	t.Run("SET statement_timeout blocked", func(t *testing.T) {
		assert.Equal(t, "SET statement_timeout is unsupported", Check("SET statement_timeout = 5000"))
	})

	t.Run("SET LOCAL statement_timeout blocked", func(t *testing.T) {
		assert.Equal(t, "SET statement_timeout is unsupported", Check("SET LOCAL statement_timeout = 5000"))
	})

	t.Run("SET lock_timeout blocked", func(t *testing.T) {
		assert.Equal(t, "SET lock_timeout is unsupported", Check("SET lock_timeout = 5000"))
	})

	t.Run("SET idle_in_transaction_session_timeout blocked", func(t *testing.T) {
		assert.Contains(t, Check("SET idle_in_transaction_session_timeout = 5000"), "unsupported")
	})

	t.Run("SET TimeZone allowed", func(t *testing.T) {
		assert.Empty(t, Check("SET TimeZone = 'UTC'"))
	})

	t.Run("isolation level READ COMMITTED blocked", func(t *testing.T) {
		assert.Equal(t, "only ISOLATION LEVEL REPEATABLE READ is supported", Check("BEGIN ISOLATION LEVEL READ COMMITTED"))
	})

	t.Run("isolation level SERIALIZABLE blocked", func(t *testing.T) {
		assert.Equal(t, "only ISOLATION LEVEL REPEATABLE READ is supported", Check("SET TRANSACTION ISOLATION LEVEL SERIALIZABLE"))
	})

	t.Run("isolation level REPEATABLE READ allowed", func(t *testing.T) {
		assert.Empty(t, Check("BEGIN ISOLATION LEVEL REPEATABLE READ"))
	})

	t.Run("bare ANALYZE blocked", func(t *testing.T) {
		assert.Equal(t, "ANALYZE requires a table name", Check("ANALYZE"))
	})

	t.Run("ANALYZE with table allowed", func(t *testing.T) {
		assert.Empty(t, Check("ANALYZE users"))
	})
}

func TestTxState(t *testing.T) {
	t.Run("no warning outside transaction", func(t *testing.T) {
		var tx TxState
		assert.Empty(t, tx.TrackTx("CREATE TABLE t (id int)"))
		assert.Empty(t, tx.TrackTx("INSERT INTO t VALUES (1)"))
	})

	t.Run("single DDL in transaction ok", func(t *testing.T) {
		var tx TxState
		assert.Empty(t, tx.TrackTx("BEGIN"))
		assert.Empty(t, tx.TrackTx("CREATE TABLE t (id int)"))
		assert.Empty(t, tx.TrackTx("COMMIT"))
	})

	t.Run("multiple DDL warns", func(t *testing.T) {
		var tx TxState
		assert.Empty(t, tx.TrackTx("BEGIN"))
		assert.Empty(t, tx.TrackTx("CREATE TABLE a (id int)"))
		assert.Contains(t, tx.TrackTx("CREATE TABLE b (id int)"), "multiple DDL")
	})

	t.Run("mixed DDL then DML warns", func(t *testing.T) {
		var tx TxState
		assert.Empty(t, tx.TrackTx("BEGIN"))
		assert.Empty(t, tx.TrackTx("CREATE TABLE t (id int)"))
		assert.Contains(t, tx.TrackTx("INSERT INTO t VALUES (1)"), "mixed DDL and DML")
	})

	t.Run("mixed DML then DDL warns", func(t *testing.T) {
		var tx TxState
		assert.Empty(t, tx.TrackTx("BEGIN"))
		assert.Empty(t, tx.TrackTx("INSERT INTO t VALUES (1)"))
		assert.Contains(t, tx.TrackTx("CREATE TABLE t (id int)"), "mixed DDL and DML")
	})

	t.Run("reset after commit", func(t *testing.T) {
		var tx TxState
		assert.Empty(t, tx.TrackTx("BEGIN"))
		assert.Empty(t, tx.TrackTx("CREATE TABLE a (id int)"))
		assert.Empty(t, tx.TrackTx("COMMIT"))
		assert.Empty(t, tx.TrackTx("BEGIN"))
		assert.Empty(t, tx.TrackTx("CREATE TABLE b (id int)"))
	})

	t.Run("reset after rollback", func(t *testing.T) {
		var tx TxState
		assert.Empty(t, tx.TrackTx("BEGIN"))
		assert.Empty(t, tx.TrackTx("CREATE TABLE a (id int)"))
		assert.Empty(t, tx.TrackTx("ROLLBACK"))
		assert.Empty(t, tx.TrackTx("BEGIN"))
		assert.Empty(t, tx.TrackTx("CREATE TABLE b (id int)"))
	})

	t.Run("SELECT does not trigger warnings", func(t *testing.T) {
		var tx TxState
		assert.Empty(t, tx.TrackTx("BEGIN"))
		assert.Empty(t, tx.TrackTx("CREATE TABLE t (id int)"))
		assert.Empty(t, tx.TrackTx("SELECT 1"))
	})

	t.Run("START TRANSACTION recognized", func(t *testing.T) {
		var tx TxState
		assert.Empty(t, tx.TrackTx("START TRANSACTION"))
		assert.Empty(t, tx.TrackTx("CREATE TABLE a (id int)"))
		assert.Contains(t, tx.TrackTx("DROP TABLE b"), "multiple DDL")
	})
}

func TestSplitStatements(t *testing.T) {
	t.Run("single statement", func(t *testing.T) {
		stmts := splitStatements("SELECT 1")
		assert.Equal(t, []string{"SELECT 1"}, stmts)
	})

	t.Run("multiple statements", func(t *testing.T) {
		stmts := splitStatements("SELECT 1; SELECT 2; SELECT 3")
		assert.Equal(t, []string{"SELECT 1", " SELECT 2", " SELECT 3"}, stmts)
	})

	t.Run("semicolon in string literal", func(t *testing.T) {
		stmts := splitStatements("SELECT 'a;b'; SELECT 2")
		assert.Len(t, stmts, 2)
		assert.Equal(t, "SELECT 'a;b'", stmts[0])
	})

	t.Run("escaped quotes in string", func(t *testing.T) {
		stmts := splitStatements("SELECT 'it''s'; SELECT 2")
		assert.Len(t, stmts, 2)
		assert.Equal(t, "SELECT 'it''s'", stmts[0])
	})

	t.Run("empty trailing statement ignored", func(t *testing.T) {
		stmts := splitStatements("SELECT 1;")
		assert.Equal(t, []string{"SELECT 1"}, stmts)
	})
}
