package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeType(t *testing.T) {
	t.Run("type aliases", func(t *testing.T) {
		cases := []struct {
			input    string
			expected string
		}{
			{"int", "integer"},
			{"INT", "integer"},
			{"int4", "integer"},
			{"int8", "bigint"},
			{"int2", "smallint"},
			{"float4", "real"},
			{"float8", "double precision"},
			{"float", "double precision"},
			{"bool", "boolean"},
			{"BOOL", "boolean"},
			{"timestamp with time zone", "timestamptz"},
			{"TIMESTAMP WITH TIME ZONE", "timestamptz"},
			{"timestamp without time zone", "timestamp"},
			{"time with time zone", "timetz"},
			{"time without time zone", "time"},
		}
		for _, c := range cases {
			t.Run(c.input, func(t *testing.T) {
				assert.Equal(t, c.expected, NormalizeType(c.input))
			})
		}
	})

	t.Run("character varying", func(t *testing.T) {
		assert.Equal(t, "varchar(255)", NormalizeType("character varying(255)"))
		assert.Equal(t, "varchar(100)", NormalizeType("CHARACTER VARYING(100)"))
	})

	t.Run("character fixed", func(t *testing.T) {
		assert.Equal(t, "char(10)", NormalizeType("character(10)"))
		assert.Equal(t, "char(1)", NormalizeType("CHARACTER(1)"))
	})

	t.Run("passthrough", func(t *testing.T) {
		assert.Equal(t, "text", NormalizeType("TEXT"))
		assert.Equal(t, "integer", NormalizeType("integer"))
		assert.Equal(t, "varchar(50)", NormalizeType("varchar(50)"))
		assert.Equal(t, "timestamptz", NormalizeType("timestamptz"))
	})

	t.Run("whitespace trimming", func(t *testing.T) {
		assert.Equal(t, "integer", NormalizeType("  int  "))
		assert.Equal(t, "text", NormalizeType("  TEXT  "))
	})
}

func TestNormalizeDefault(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		assert.Equal(t, "", normalizeDefault(""))
	})

	t.Run("simple literal", func(t *testing.T) {
		assert.Equal(t, "42", normalizeDefault("42"))
	})

	t.Run("strips trivial type cast", func(t *testing.T) {
		assert.Equal(t, "'open'", normalizeDefault("'open'::text"))
	})

	t.Run("strips outer parens with cast", func(t *testing.T) {
		assert.Equal(t, "gen_random_uuid()", normalizeDefault("(gen_random_uuid())::text"))
	})

	t.Run("lowercases", func(t *testing.T) {
		assert.Equal(t, "now()", normalizeDefault("NOW()"))
	})

	t.Run("boolean cast stripped", func(t *testing.T) {
		assert.Equal(t, "false", normalizeDefault("false::boolean"))
	})

	t.Run("integer cast stripped", func(t *testing.T) {
		assert.Equal(t, "0", normalizeDefault("0::integer"))
	})

	t.Run("no-op when no cast", func(t *testing.T) {
		assert.Equal(t, "now()", normalizeDefault("now()"))
	})

	t.Run("preserves non-trivial cast", func(t *testing.T) {
		assert.Equal(t, "foo::jsonb", normalizeDefault("foo::jsonb"))
	})

	t.Run("nested parens with cast", func(t *testing.T) {
		result := normalizeDefault("((gen_random_uuid()))::text")
		assert.Equal(t, "gen_random_uuid()", result)
	})

	t.Run("expression with comma not stripped", func(t *testing.T) {
		result := normalizeDefault("(1, 2)")
		assert.Equal(t, "(1, 2)", result)
	})
}

func TestNormalizeCheck(t *testing.T) {
	t.Run("simple expression", func(t *testing.T) {
		assert.Equal(t, "x > 0", normalizeCheck("(x > 0)"))
	})

	t.Run("strips type casts", func(t *testing.T) {
		result := normalizeCheck("(status = 'open'::text)")
		assert.Equal(t, "status = 'open'", result)
	})

	t.Run("converts ANY ARRAY to IN", func(t *testing.T) {
		result := normalizeCheck("(status = ANY (ARRAY['open', 'done']))")
		assert.Equal(t, "status in ('open', 'done')", result)
	})

	t.Run("double outer parens stripped", func(t *testing.T) {
		result := normalizeCheck("((x > 0))")
		assert.Equal(t, "x > 0", result)
	})

	t.Run("normalizes whitespace", func(t *testing.T) {
		result := normalizeCheck("(  x   >   0  )")
		assert.Equal(t, "x > 0", result)
	})

	t.Run("unbalanced inner parens preserved", func(t *testing.T) {
		result := normalizeCheck("(a > 0) AND (b < 10)")
		assert.Equal(t, "(a > 0) and (b < 10)", result)
	})

	t.Run("strips type casts with ANY ARRAY", func(t *testing.T) {
		result := normalizeCheck("(status = ANY (ARRAY['open'::text, 'done'::text]))")
		assert.Equal(t, "status in ('open', 'done')", result)
	})
}
