package verify

import (
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

	t.Run("FOREIGN KEY detected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "bad.sql", `CREATE TABLE t (id TEXT, parent_id TEXT REFERENCES parents(id));`)
		violations, err := Files([]string{dir})
		require.NoError(t, err)
		assert.NotEmpty(t, violations)

		hasFk := false
		for _, v := range violations {
			if v.Rule == "FOREIGN KEY constraint not supported" {
				hasFk = true
			}
		}
		assert.True(t, hasFk)
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
		assert.Contains(t, violations[0].Rule, "non-SQL language")
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
