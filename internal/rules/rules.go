package rules

import (
	"regexp"
	"strings"
)

// Rule represents a single DSQL compatibility check rule.
type Rule struct {
	Name    string
	Pattern *regexp.Regexp
	// Dynamic indicates the Name contains %s for a captured group.
	Dynamic bool
}

// Match returns the violation message if the rule matches the SQL string,
// or "" if it does not match.
func (r Rule) Match(sql string) string {
	if r.Dynamic {
		m := r.Pattern.FindStringSubmatch(sql)
		if m == nil {
			return ""
		}
		return strings.Replace(r.Name, "%s", m[len(m)-1], 1)
	}
	if r.Pattern.MatchString(sql) {
		return r.Name
	}
	return ""
}

// SharedRules contains DSQL compatibility rules shared across verify and proxy.
var SharedRules = []Rule{
	// Unsupported CREATE statements
	{Name: "CREATE DATABASE statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+DATABASE\b`)},
	{Name: "CREATE EXTENSION statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+EXTENSION\b`)},
	{Name: "CREATE TRIGGER statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?TRIGGER\b`)},
	{Name: "CREATE TABLESPACE statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+TABLESPACE\b`)},
	{Name: "CREATE MATERIALIZED VIEW or CREATE TABLE AS statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+MATERIALIZED\s+VIEW\b`)},
	{Name: "CREATE TYPE (composite types) statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+TYPE\b`)},

	// Unsupported table options
	{Name: "CREATE TEMPORARY TABLE is unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+(GLOBAL\s+|LOCAL\s+)?TEMP(ORARY)?\s+TABLE\b`)},
	{Name: "CREATE TABLE INHERITS is unsupported", Pattern: regexp.MustCompile(`(?i)\bINHERITS\s*\(`)},
	{Name: "CREATE TABLE PARTITION is unsupported", Pattern: regexp.MustCompile(`(?i)\bPARTITION\s+BY\b`)},

	// Unsupported statements
	{Name: "TRUNCATE TABLE statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bTRUNCATE\b`)},

	// Function language check (dynamic: %s is replaced with the captured language name)
	{Name: "CREATE FUNCTION with language %s not supported", Pattern: regexp.MustCompile(`(?i)\bLANGUAGE\s+(plpgsql|plv8|plperl|plpython|plpython3u|pltcl|c)\b`), Dynamic: true},
}
