package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSharedRules(t *testing.T) {
	t.Run("expected rule count", func(t *testing.T) {
		assert.Len(t, SharedRules, 11)
	})

	t.Run("static rules match expected SQL", func(t *testing.T) {
		cases := []struct {
			sql  string
			want string
		}{
			{"CREATE DATABASE mydb", "CREATE DATABASE statements are unsupported"},
			{"CREATE EXTENSION pgcrypto", "CREATE EXTENSION statements are unsupported"},
			{"CREATE TRIGGER trg AFTER INSERT ON t", "CREATE TRIGGER statements are unsupported"},
			{"CREATE OR REPLACE TRIGGER trg AFTER INSERT ON t", "CREATE TRIGGER statements are unsupported"},
			{"CREATE TABLESPACE ts LOCATION '/data'", "CREATE TABLESPACE statements are unsupported"},
			{"CREATE MATERIALIZED VIEW mv AS SELECT 1", "CREATE MATERIALIZED VIEW or CREATE TABLE AS statements are unsupported"},
			{"CREATE TYPE address AS (street text)", "CREATE TYPE (composite types) statements are unsupported"},
			{"CREATE TEMPORARY TABLE tmp (id int)", "CREATE TEMPORARY TABLE is unsupported"},
			{"CREATE TEMP TABLE tmp (id int)", "CREATE TEMPORARY TABLE is unsupported"},
			{"CREATE TABLE child (extra text) INHERITS (parent)", "CREATE TABLE INHERITS is unsupported"},
			{"CREATE TABLE t (id int) PARTITION BY RANGE (id)", "CREATE TABLE PARTITION is unsupported"},
			{"TRUNCATE TABLE users", "TRUNCATE TABLE statements are unsupported"},
		}
		for _, tc := range cases {
			matched := false
			for _, r := range SharedRules {
				if msg := r.Match(tc.sql); msg != "" {
					assert.Equal(t, tc.want, msg, "sql: %s", tc.sql)
					matched = true
					break
				}
			}
			assert.True(t, matched, "expected match for: %s", tc.sql)
		}
	})

	t.Run("dynamic LANGUAGE rule", func(t *testing.T) {
		assert.Equal(t, "CREATE FUNCTION with language plpgsql not supported",
			SharedRules[len(SharedRules)-1].Match("CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$ BEGIN END $$"))
		assert.Equal(t, "CREATE FUNCTION with language plv8 not supported",
			SharedRules[len(SharedRules)-1].Match("CREATE FUNCTION f() RETURNS void LANGUAGE plv8 AS $$ $$"))
	})

	t.Run("safe SQL does not match", func(t *testing.T) {
		safe := []string{
			"SELECT 1",
			"INSERT INTO users (id) VALUES ('1')",
			"CREATE TABLE t (id TEXT PRIMARY KEY)",
			"CREATE INDEX ASYNC idx ON t (col)",
			"CREATE FUNCTION add(a int, b int) RETURNS int LANGUAGE SQL AS 'SELECT a + b'",
		}
		for _, sql := range safe {
			for _, r := range SharedRules {
				assert.Empty(t, r.Match(sql), "rule %q should not match %q", r.Name, sql)
			}
		}
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		assert.NotEmpty(t, SharedRules[0].Match("create database mydb"))
		assert.NotEmpty(t, SharedRules[0].Match("CREATE DATABASE mydb"))
	})
}
