package schema

import (
	"regexp"
	"strings"
)

var typeAliases = map[string]string{
	"int":                         "integer",
	"int4":                        "integer",
	"int8":                        "bigint",
	"int2":                        "smallint",
	"float4":                      "real",
	"float8":                      "double precision",
	"float":                       "double precision",
	"bool":                        "boolean",
	"decimal":                     "numeric",
	"character varying":           "varchar",
	"timestamp with time zone":    "timestamptz",
	"timestamp without time zone": "timestamp",
	"time with time zone":         "timetz",
	"time without time zone":      "time",
}

var charVaryingRe = regexp.MustCompile(`(?i)^character varying\((\d+)\)$`)
var charFixedRe = regexp.MustCompile(`(?i)^character\((\d+)\)$`)

var typeParenSpaceRe = regexp.MustCompile(`\s*([(),])\s*`)

// NormalizeType converts a SQL type to its canonical lowercase form.
func NormalizeType(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))
	s = strings.Join(strings.Fields(s), " ")
	s = typeParenSpaceRe.ReplaceAllString(s, "$1")
	s = strings.TrimSpace(s)

	if m := charVaryingRe.FindStringSubmatch(s); m != nil {
		return "varchar(" + m[1] + ")"
	}
	if m := charFixedRe.FindStringSubmatch(s); m != nil {
		return "char(" + m[1] + ")"
	}

	if canonical, ok := typeAliases[s]; ok {
		return canonical
	}

	return s
}

// normalizeDefault normalizes a column default expression for comparison.
// PostgreSQL's pg_get_expr wraps function calls in parentheses and uses
// lowercase types, while parsed SQL may not.
func normalizeDefault(d string) string {
	s := strings.TrimSpace(strings.ToLower(d))
	// Strip redundant outer parentheses: ((expr))::type → (expr)::type
	for strings.HasPrefix(s, "(") && !strings.Contains(s, ",") {
		// Find the matching closing paren
		depth := 0
		closeIdx := -1
		for i, ch := range s {
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
				if depth == 0 {
					closeIdx = i
					break
				}
			}
		}
		// Only strip if the paren wraps a complete sub-expression
		// e.g., "(gen_random_uuid())::text" → strip outer parens if they wrap just the function call
		if closeIdx < 0 || closeIdx == len(s)-1 {
			break
		}
		// Check what follows the closing paren
		rest := s[closeIdx+1:]
		if strings.HasPrefix(rest, "::") {
			// (expr)::type → strip parens: expr::type
			inner := s[1:closeIdx]
			s = inner + rest
		} else {
			break
		}
	}

	// Strip trivial type casts that PostgreSQL adds (e.g., 'open'::text).
	s = trivialCastRe.ReplaceAllString(s, "")

	return s
}

// Strip type casts. Matches ::type including multi-word types like "character varying",
// "double precision", "timestamp with time zone", types with precision like "numeric(10,2)",
// and array casts like "::text[]".
var trivialCastRe = regexp.MustCompile(`::(?:character varying|double precision|timestamp with(?:out)? time zone|time with(?:out)? time zone|text|integer|bigint|smallint|boolean|real|numeric|timestamptz|timestamp|date|timetz|time|uuid|bytea|varchar|char|regclass|jsonb|json)(?:\(\d+(?:,\s*\d+)?\))?(?:\[\])?`)

// normalizeCheck normalizes a CHECK constraint expression for comparison.
// PostgreSQL rewrites IN (...) to = ANY (ARRAY[...]) and adds type casts.
// We normalize both forms to a comparable string.
var (
	anyArrayRe = regexp.MustCompile(`(?i)=\s*ANY\s*\(+ARRAY\[([^\]]+)\]\)+`)
	// Matches: <col> BETWEEN <lo> AND <hi>
	betweenRe = regexp.MustCompile(`(?i)(\w+)\s+between\s+(\S+)\s+and\s+(\S+)`)
)

func normalizeCheck(expr string) string {
	s := strings.TrimSpace(strings.ToLower(expr))

	// Strip type casts inside the expression.
	s = trivialCastRe.ReplaceAllString(s, "")

	// Convert = ANY (ARRAY[...]) back to IN (...)
	s = anyArrayRe.ReplaceAllStringFunc(s, func(match string) string {
		m := anyArrayRe.FindStringSubmatch(match)
		if m == nil {
			return match
		}
		return "in (" + m[1] + ")"
	})

	// Expand BETWEEN x AND y to >= x and <= y (DSQL stores the expanded form).
	s = betweenRe.ReplaceAllString(s, "$1 >= $2 and $1 <= $3")

	// Strip ALL parentheses and brackets for comparison. DSQL and PostgreSQL
	// add varying amounts of parens around sub-expressions, and ARRAY[...]
	// may leave brackets after conversion.
	s = strings.ReplaceAll(s, "(", "")
	s = strings.ReplaceAll(s, ")", "")
	s = strings.ReplaceAll(s, "[", "")
	s = strings.ReplaceAll(s, "]", "")

	// Normalize whitespace.
	s = strings.Join(strings.Fields(s), " ")

	return s
}

// typesEqual compares two normalized types. Aurora DSQL stores an
// unconstrained NUMERIC as numeric(18,6), so the two are treated as equal.
func typesEqual(a, b string) bool {
	a, b = NormalizeType(a), NormalizeType(b)
	if a == b {
		return true
	}
	isDefaultNumeric := func(s string) bool { return s == "numeric" || s == "numeric(18,6)" }
	return isDefaultNumeric(a) && isDefaultNumeric(b)
}

// normalizeExpr normalizes an arbitrary SQL expression (index key, partial
// index predicate, generated column, statistics expression) for comparison.
func normalizeExpr(expr string) string {
	return normalizeCheck(expr)
}

var (
	trailingAscRe       = regexp.MustCompile(`\s+asc$`)
	trailingNullsLastRe = regexp.MustCompile(`\s+nulls\s+last$`)
)

// normalizeIndexElem normalizes an index key element for comparison. ASC and
// NULLS LAST are the defaults for ascending btree keys and are dropped.
func normalizeIndexElem(elem string) string {
	s := strings.Join(strings.Fields(strings.ToLower(elem)), " ")
	if !strings.Contains(s, " desc") {
		s = trailingNullsLastRe.ReplaceAllString(s, "")
	}
	s = trailingAscRe.ReplaceAllString(s, "")
	return normalizeExpr(s)
}
