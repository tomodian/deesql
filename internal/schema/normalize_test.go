package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeType(t *testing.T) {
	t.Run("integer aliases", func(t *testing.T) {
		assert.Equal(t, "integer", NormalizeType("int"))
		assert.Equal(t, "integer", NormalizeType("int4"))
		assert.Equal(t, "integer", NormalizeType("INTEGER"))
	})

	t.Run("bigint aliases", func(t *testing.T) {
		assert.Equal(t, "bigint", NormalizeType("int8"))
		assert.Equal(t, "bigint", NormalizeType("BIGINT"))
	})

	t.Run("smallint aliases", func(t *testing.T) {
		assert.Equal(t, "smallint", NormalizeType("int2"))
	})

	t.Run("boolean aliases", func(t *testing.T) {
		assert.Equal(t, "boolean", NormalizeType("bool"))
		assert.Equal(t, "boolean", NormalizeType("BOOLEAN"))
	})

	t.Run("double precision aliases", func(t *testing.T) {
		assert.Equal(t, "double precision", NormalizeType("float8"))
	})

	t.Run("real aliases", func(t *testing.T) {
		assert.Equal(t, "real", NormalizeType("float4"))
	})

	t.Run("timestamptz aliases", func(t *testing.T) {
		assert.Equal(t, "timestamptz", NormalizeType("timestamp with time zone"))
		assert.Equal(t, "timestamptz", NormalizeType("TIMESTAMPTZ"))
	})

	t.Run("character varying aliases", func(t *testing.T) {
		assert.Equal(t, "varchar(100)", NormalizeType("character varying(100)"))
	})

	t.Run("character aliases", func(t *testing.T) {
		assert.Equal(t, "char(10)", NormalizeType("character(10)"))
	})

	t.Run("numeric unchanged", func(t *testing.T) {
		assert.Equal(t, "numeric(10,2)", NormalizeType("numeric(10,2)"))
	})

	t.Run("text unchanged", func(t *testing.T) {
		assert.Equal(t, "text", NormalizeType("text"))
	})
}

func TestNormalizeDefault(t *testing.T) {
	t.Run("string literal unchanged", func(t *testing.T) {
		assert.Equal(t, "'open'", normalizeDefault("'open'"))
	})

	t.Run("strips type cast", func(t *testing.T) {
		assert.Equal(t, "'open'", normalizeDefault("'open'::text"))
	})

	t.Run("parens preserved", func(t *testing.T) {
		assert.Equal(t, "(0)", normalizeDefault("(0)"))
	})

	t.Run("function unchanged", func(t *testing.T) {
		assert.Equal(t, "now()", normalizeDefault("now()"))
	})

	t.Run("gen_random_uuid cast stripped", func(t *testing.T) {
		assert.Equal(t, "gen_random_uuid()", normalizeDefault("(gen_random_uuid())::text"))
	})

	t.Run("empty string", func(t *testing.T) {
		assert.Equal(t, "", normalizeDefault(""))
	})

	t.Run("boolean", func(t *testing.T) {
		assert.Equal(t, "true", normalizeDefault("TRUE"))
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
		assert.Equal(t, "status in 'open', 'done'", result)
	})

	t.Run("normalizes whitespace", func(t *testing.T) {
		result := normalizeCheck("(  x   >   0  )")
		assert.Equal(t, "x > 0", result)
	})

	t.Run("AND with sub-expressions", func(t *testing.T) {
		result := normalizeCheck("(a > 0) AND (b < 10)")
		assert.Equal(t, "a > 0 and b < 10", result)
	})

	t.Run("strips type casts with ANY ARRAY", func(t *testing.T) {
		result := normalizeCheck("(status = ANY (ARRAY['open'::text, 'done'::text]))")
		assert.Equal(t, "status in 'open', 'done'", result)
	})

	t.Run("DSQL rating check normalization", func(t *testing.T) {
		// Parsed: (rating >= 0 AND rating <= 5)
		parsed := normalizeCheck("(rating >= 0 AND rating <= 5)")
		// DSQL introspected: ((rating >= (0)) AND (rating <= (5)))
		introspected := normalizeCheck("((rating >= (0)) AND (rating <= (5)))")
		assert.Equal(t, parsed, introspected)
	})

	t.Run("DSQL IN normalization", func(t *testing.T) {
		// Parsed: (status IN ('open', 'done'))
		parsed := normalizeCheck("(status IN ('open', 'done'))")
		// DSQL introspected: (status = ANY (ARRAY['open'::text, 'done'::text]))
		introspected := normalizeCheck("(status = ANY (ARRAY['open'::text, 'done'::text]))")
		assert.Equal(t, parsed, introspected)
	})

	t.Run("DSQL BETWEEN normalization", func(t *testing.T) {
		// Parsed from SQL: (priority BETWEEN 0 AND 4)
		parsed := normalizeCheck("(priority BETWEEN 0 AND 4)")
		// DSQL introspected: ((priority >= (0)) AND (priority <= (4)))
		introspected := normalizeCheck("((priority >= (0)) AND (priority <= (4)))")
		assert.Equal(t, parsed, introspected)
	})

	t.Run("DSQL budget check normalization", func(t *testing.T) {
		parsed := normalizeCheck("(budget >= 0)")
		introspected := normalizeCheck("((budget >= (0)))")
		assert.Equal(t, parsed, introspected)
	})

	t.Run("DSQL varchar IN with column cast and array cast", func(t *testing.T) {
		// Actual DSQL pg_get_constraintdef output for CHECK (action IN ('create', 'update', ...))
		parsed := normalizeCheck("(action IN ('create', 'update', 'delete', 'login', 'logout'))")
		introspected := normalizeCheck("(((action)::text = ANY ((ARRAY['create'::character varying, 'update'::character varying, 'delete'::character varying, 'login'::character varying, 'logout'::character varying])::text[])))")
		assert.Equal(t, parsed, introspected)
	})
}
