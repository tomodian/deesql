package schema

import (
	"regexp"
	"strings"
)

var typeAliases = map[string]string{
	"int":                           "integer",
	"int4":                          "integer",
	"int8":                          "bigint",
	"int2":                          "smallint",
	"float4":                        "real",
	"float8":                        "double precision",
	"float":                         "double precision",
	"bool":                          "boolean",
	"timestamp with time zone":      "timestamptz",
	"timestamp without time zone":   "timestamp",
	"time with time zone":           "timetz",
	"time without time zone":        "time",
}

var charVaryingRe = regexp.MustCompile(`(?i)^character varying\((\d+)\)$`)
var charFixedRe = regexp.MustCompile(`(?i)^character\((\d+)\)$`)

// NormalizeType converts a SQL type to its canonical lowercase form.
func NormalizeType(raw string) string {
	s := strings.TrimSpace(strings.ToLower(raw))

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

var trivialCastRe = regexp.MustCompile(`::(?:text|integer|bigint|smallint|boolean|real|double precision|numeric|timestamptz|timestamp|date|time|timetz|uuid|bytea|varchar|char)\b`)

// normalizeCheck normalizes a CHECK constraint expression for comparison.
// PostgreSQL rewrites IN (...) to = ANY (ARRAY[...]) and adds type casts.
// We normalize both forms to a comparable string.
var anyArrayRe = regexp.MustCompile(`(?i)=\s*ANY\s*\(ARRAY\[([^\]]+)\]\)`)

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

	// Strip redundant outer parens: ((expr)) → (expr)
	for len(s) > 2 && s[0] == '(' && s[len(s)-1] == ')' {
		// Check if the outer parens are redundant (match each other).
		inner := s[1 : len(s)-1]
		depth := 0
		balanced := true
		for _, ch := range inner {
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
			}
			if depth < 0 {
				balanced = false
				break
			}
		}
		if balanced && depth == 0 {
			s = inner
		} else {
			break
		}
	}

	// Normalize whitespace.
	s = strings.Join(strings.Fields(s), " ")

	return s
}
