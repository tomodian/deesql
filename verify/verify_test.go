package verify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomodian/deesql/internal/rules"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckSQL(t *testing.T) {
	t.Run("2026 DSQL features are accepted", func(t *testing.T) {
		violations := CheckSQL("ok.sql", `
CREATE SEQUENCE s CACHE 65536;
CREATE TABLE p (id BIGINT GENERATED ALWAYS AS IDENTITY (CACHE 1) PRIMARY KEY, doc JSONB, raw JSON);
CREATE TABLE c (
    id   BIGINT PRIMARY KEY DEFAULT nextval('s'),
    p_id BIGINT REFERENCES p (id) ON DELETE CASCADE,
    n    INTEGER,
    g    INTEGER GENERATED ALWAYS AS (n * 2) STORED,
    FOREIGN KEY (p_id) REFERENCES p (id)
);
CREATE INDEX ASYNC idx_c ON c ((n + 1), p_id NULLS FIRST) INCLUDE (g) WHERE n > 0;
CREATE UNIQUE INDEX ASYNC idx_u ON c (n) NULLS NOT DISTINCT;
CREATE STATISTICS st (ndistinct) ON n, p_id FROM c;
ALTER TABLE c ADD CONSTRAINT c_n CHECK (n > 0) NOT VALID;
ALTER TABLE c ADD CONSTRAINT c_fk FOREIGN KEY (p_id) REFERENCES p (id) NOT VALID;
ALTER TABLE ASYNC c VALIDATE CONSTRAINT c_n;
ALTER TABLE c ADD CONSTRAINT c_u UNIQUE USING INDEX idx_u;
ALTER TABLE c DROP COLUMN g, ALTER COLUMN n DROP NOT NULL, ALTER COLUMN n SET DEFAULT 1;
ALTER SYSTEM SET dsql.enable_batched_nestloop = off;
`)
		assert.Empty(t, violations)
	})

	t.Run("statement-level violations report the first line and context", func(t *testing.T) {
		violations := CheckSQL("bad.sql", "SELECT 1;\n\nALTER TABLE t\n  ADD CONSTRAINT c\n  CHECK (a > 0);\n")
		require.Len(t, violations, 1)
		assert.Equal(t, Violation{File: "bad.sql", Line: 3, Rule: rules.MsgAddConstraintValid, Context: "ALTER TABLE t"}, violations[0])
	})

	t.Run("every statement rule is reachable", func(t *testing.T) {
		cases := map[string]string{
			"CREATE INDEX i ON t (a);":                                        rules.MsgIndexAsync,
			"CREATE INDEX ASYNC i ON t (a DESC);":                             rules.MsgIndexOrdering,
			"ALTER TABLE t ADD PRIMARY KEY (a);":                              rules.MsgAddPrimaryKey,
			"ALTER TABLE t ADD UNIQUE (a);":                                   rules.MsgAddUnique,
			"ALTER TABLE t ALTER COLUMN a TYPE text;":                         rules.MsgAlterColumnType,
			"ALTER TABLE t ALTER COLUMN a SET NOT NULL;":                      rules.MsgSetNotNull,
			"CREATE SEQUENCE s;":                                              rules.MsgSequenceCache,
			"CREATE SEQUENCE s AS integer CACHE 1;":                           rules.MsgSequenceType,
			"CREATE SEQUENCE s CACHE 64;":                                     rules.MsgCacheRange,
			"CREATE TABLE t (id int GENERATED ALWAYS AS IDENTITY (CACHE 1));": rules.MsgIdentityType,
			"CREATE TABLE t (id bigint GENERATED ALWAYS AS IDENTITY);":        rules.MsgIdentityCache,
		}
		for sql, want := range cases {
			var got []string
			for _, v := range CheckSQL("x.sql", sql) {
				got = append(got, v.Rule)
			}
			assert.Equal(t, []string{want}, got, "sql: %s", sql)
		}
	})
}

func TestCheckFiles(t *testing.T) {
	t.Run("reads files and reports violations", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "a.sql")
		require.NoError(t, os.WriteFile(f, []byte("CREATE SEQUENCE s;"), 0o644))

		violations, err := CheckFiles([]string{f})
		require.NoError(t, err)
		require.Len(t, violations, 1)
		assert.Equal(t, f, violations[0].File)
	})

	t.Run("missing file returns error", func(t *testing.T) {
		_, err := CheckFiles([]string{filepath.Join(t.TempDir(), "missing.sql")})
		require.Error(t, err)
	})
}

func TestCheckDirs(t *testing.T) {
	t.Run("globs .sql files", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.sql"), []byte("CREATE SEQUENCE s CACHE 1;"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("CREATE SEQUENCE s;"), 0o644))

		violations, err := CheckDirs([]string{dir})
		require.NoError(t, err)
		assert.Empty(t, violations)
	})
}
