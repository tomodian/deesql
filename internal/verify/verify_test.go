package verify

import (
	"github.com/tomodian/deesql/internal/rules"

	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFiles(t *testing.T) {
	t.Run("valid DSQL schema has no violations", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "valid.sql", `
CREATE TABLE users (
    id   TEXT NOT NULL,
    name TEXT NOT NULL,
    PRIMARY KEY (id)
);

CREATE INDEX ASYNC idx_users_name ON users (name);
`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)
		assert.Empty(t, violations)
	})

	t.Run("CREATE EXTENSION detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `CREATE EXTENSION pgcrypto;`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)
		assert.NotEmpty(t, violations)
		assert.Contains(t, violations[0].Rule, "CREATE EXTENSION")
	})

	t.Run("CREATE TRIGGER detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `CREATE TRIGGER my_trigger AFTER INSERT ON t FOR EACH ROW EXECUTE FUNCTION f();`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)
		assert.NotEmpty(t, violations)
		assert.Contains(t, violations[0].Rule, "TRIGGER")
	})

	t.Run("SERIAL type detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `CREATE TABLE t (id SERIAL PRIMARY KEY);`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)
		assert.NotEmpty(t, violations)

		hasSerial := false
		for _, v := range violations {
			if v.Rule == "SERIAL type not supported (use GENERATED AS IDENTITY)" {
				hasSerial = true
			}
		}
		assert.True(t, hasSerial)
	})

	t.Run("BIGSERIAL detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `CREATE TABLE t (id BIGSERIAL PRIMARY KEY);`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)

		hasSerial := false
		for _, v := range violations {
			if v.Rule == "SERIAL type not supported (use GENERATED AS IDENTITY)" {
				hasSerial = true
			}
		}
		assert.True(t, hasSerial)
	})

	t.Run("newly supported DSQL features pass", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "ok.sql", `
CREATE SEQUENCE order_seq CACHE 65536;

CREATE TABLE users (
    id    BIGINT GENERATED ALWAYS AS IDENTITY (CACHE 70000) PRIMARY KEY,
    email TEXT NOT NULL,
    prefs JSONB,
    raw   JSON
);

CREATE TABLE orders (
    id      BIGINT PRIMARY KEY DEFAULT nextval('order_seq'),
    user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    qty     INTEGER NOT NULL,
    total   INTEGER GENERATED ALWAYS AS (qty * 2) STORED,
    FOREIGN KEY (user_id) REFERENCES users (id)
);

CREATE INDEX ASYNC idx_users_lower_email ON users ((lower(email)));
CREATE INDEX ASYNC idx_orders_open ON orders (user_id NULLS FIRST) WHERE qty > 0;
CREATE STATISTICS orders_stats (ndistinct) ON user_id, qty FROM orders;

ALTER TABLE orders ADD CONSTRAINT orders_qty_check CHECK (qty > 0) NOT VALID;
ALTER TABLE ASYNC orders VALIDATE CONSTRAINT orders_qty_check;
ALTER SYSTEM SET dsql.enable_batched_nestloop = off;
`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)
		assert.Empty(t, violations)
	})

	t.Run("multi-line statement rules report the statement's first line", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `CREATE TABLE t (a INT);

ALTER TABLE t
    ADD CONSTRAINT t_a_check
    CHECK (a > 0);

CREATE SEQUENCE s
    INCREMENT BY 1;

CREATE TABLE u (
    id INTEGER GENERATED ALWAYS AS IDENTITY
);
`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)

		byRule := map[string]int{}
		for _, v := range violations {
			byRule[v.Rule] = v.Line
		}
		assert.Equal(t, 3, byRule[rules.MsgAddConstraintValid])
		assert.Equal(t, "ALTER TABLE t", violations[0].Context)
		assert.Equal(t, 7, byRule[rules.MsgSequenceCache])
		assert.Equal(t, 10, byRule[rules.MsgIdentityType])
		assert.Equal(t, 10, byRule[rules.MsgIdentityCache])
	})

	t.Run("still-unsupported ALTER TABLE forms detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `
ALTER TABLE t ALTER COLUMN a TYPE BIGINT;
ALTER TABLE t ALTER COLUMN a SET NOT NULL;
ALTER TABLE t ADD PRIMARY KEY (id);
ALTER TABLE t ADD CONSTRAINT u UNIQUE (email);
CREATE SEQUENCE s CACHE 1000;
`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)

		var got []string
		for _, v := range violations {
			got = append(got, v.Rule)
		}
		assert.ElementsMatch(t, []string{
			rules.MsgAlterColumnType, rules.MsgSetNotNull, rules.MsgAddPrimaryKey, rules.MsgAddUnique, rules.MsgCacheRange,
		}, got)
	})

	t.Run("unsupported index types detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `CREATE INDEX ASYNC idx ON t USING gin (data);`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)
		assert.NotEmpty(t, violations)

		hasGin := false
		for _, v := range violations {
			if v.Rule == "only btree indexes supported (gin not supported)" {
				hasGin = true
			}
		}
		assert.True(t, hasGin)
	})

	t.Run("CREATE INDEX without ASYNC detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `CREATE INDEX idx_t ON t (col);`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)

		hasAsync := false
		for _, v := range violations {
			if v.Rule == "CREATE INDEX must use ASYNC (use CREATE INDEX ASYNC)" {
				hasAsync = true
			}
		}
		assert.True(t, hasAsync)
	})

	t.Run("CREATE INDEX CONCURRENTLY detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `CREATE INDEX CONCURRENTLY idx_t ON t (col);`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)

		hasConcurrent := false
		for _, v := range violations {
			if v.Rule == "CREATE INDEX CONCURRENTLY not supported (use CREATE INDEX ASYNC)" {
				hasConcurrent = true
			}
		}
		assert.True(t, hasConcurrent)
	})

	t.Run("PARTITION BY detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `CREATE TABLE t (id TEXT) PARTITION BY HASH (id);`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)
		assert.NotEmpty(t, violations)
	})

	t.Run("TRUNCATE detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `TRUNCATE TABLE users;`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)
		assert.NotEmpty(t, violations)
		assert.Contains(t, violations[0].Rule, "TRUNCATE")
	})

	t.Run("non-SQL language detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$ BEGIN END; $$;`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)
		assert.NotEmpty(t, violations)
		assert.Contains(t, violations[0].Rule, "language plpgsql")
	})

	t.Run("comments are skipped", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "ok.sql", `
-- CREATE EXTENSION pgcrypto;
-- SERIAL is fine in comments
CREATE TABLE t (id TEXT NOT NULL);
`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)
		assert.Empty(t, violations)
	})

	t.Run("multiple directories", func(t *testing.T) {
		dir1 := t.TempDir()
		dir2 := t.TempDir()
		writeFile(t, dir1, "a.sql", `CREATE TABLE a (id TEXT);`)
		writeFile(t, dir2, "b.sql", `CREATE TABLE b (id SERIAL);`)

		violations, err := Files([]string{dir1, dir2})
		require.NoError(t, err)
		assert.NotEmpty(t, violations)
		assert.Equal(t, filepath.Join(dir2, "b.sql"), violations[0].File)
	})

	t.Run("violation has correct line number", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", "line1\nline2\nCREATE EXTENSION foo;\nline4\n")
		violations, err := Files([]string{dir})
		require.NoError(t, err)
		require.NotEmpty(t, violations)
		assert.Equal(t, 3, violations[0].Line)
	})

	t.Run("unsupported types detected", func(t *testing.T) {
		types := []string{"money", "xml", "cidr", "macaddr", "hstore", "tsvector", "tsquery", "int4range"}
		for _, typ := range types {
			t.Run(typ, func(t *testing.T) {
				dir := t.TempDir()
				writeFile(t, dir, "bad.sql", "CREATE TABLE t (col "+typ+" NOT NULL);")
				violations, err := Files([]string{dir})
				require.NoError(t, err)
				assert.NotEmpty(t, violations, "expected violation for type %s", typ)
			})
		}
	})

	t.Run("fixture files pass verification", func(t *testing.T) {
		fixtureDir := filepath.Join("..", "..", "tests", "simple")
		if _, err := os.Stat(fixtureDir); os.IsNotExist(err) {
			t.Skip("fixture directory not found")
		}

		violations, err := Files([]string{fixtureDir})
		require.NoError(t, err)
		assert.Empty(t, violations)
	})
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
	require.NoError(t, err)
}
