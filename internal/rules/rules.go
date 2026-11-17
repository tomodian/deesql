package rules

import (
	"regexp"
	"strings"
)

// Rule represents a single DSQL compatibility check rule.
type Rule struct {
	Name    string
	Pattern *regexp.Regexp
	// Unless suppresses the rule when it also matches (Go regexp has no
	// negative lookahead, so "X without Y" is expressed as Pattern + Unless).
	Unless *regexp.Regexp
	// Dynamic indicates the Name contains %s for a captured group.
	Dynamic bool
	// Func is an optional custom check used instead of Pattern. It returns
	// the violation message, or "" when the SQL is fine.
	Func func(sql string) string
}

// Match returns the violation message if the rule matches the SQL string,
// or "" if it does not match.
func (r Rule) Match(sql string) string {
	if r.Func != nil {
		return r.Func(sql)
	}
	if r.Unless != nil && r.Unless.MatchString(sql) {
		return ""
	}
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
// They are safe to evaluate line-by-line.
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
	{Name: "CREATE TABLE PARTITION is unsupported", Pattern: regexp.MustCompile(`(?i)\)\s*PARTITION\s+BY\b`)},

	// Unsupported statements
	{Name: "TRUNCATE TABLE statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bTRUNCATE\b`)},

	// Function language check (dynamic: %s is replaced with the captured language name)
	{Name: "CREATE FUNCTION with language %s not supported", Pattern: regexp.MustCompile(`(?i)\bLANGUAGE\s+(plpgsql|plv8|plperl|plpython|plpython3u|pltcl|c)\b`), Dynamic: true},
}

// Rule messages referenced by tests and consumers.
const (
	MsgIndexAsync         = "CREATE INDEX must use ASYNC (use CREATE INDEX ASYNC)"
	MsgIndexOrdering      = "CREATE Index with ordering ASC or DESC is unsupported"
	MsgAddConstraintValid = "ALTER TABLE ADD CHECK/FOREIGN KEY requires NOT VALID (then run ALTER TABLE ASYNC ... VALIDATE CONSTRAINT)"
	MsgAddPrimaryKey      = "ALTER TABLE ADD PRIMARY KEY is unsupported (define the primary key in CREATE TABLE)"
	MsgAddUnique          = "ALTER TABLE ADD UNIQUE requires USING INDEX (build it first with CREATE UNIQUE INDEX ASYNC)"
	MsgAlterColumnType    = "ALTER TABLE ALTER COLUMN TYPE is unsupported"
	MsgSetNotNull         = "ALTER TABLE ALTER COLUMN SET NOT NULL is unsupported"
	MsgSequenceCache      = "CREATE SEQUENCE requires an explicit CACHE (1 or >= 65536)"
	MsgCacheRange         = "sequence CACHE must be 1 or >= 65536"
	MsgSequenceType       = "sequences only support AS bigint"
	MsgIdentityType       = "identity columns must be bigint"
	MsgIdentityCache      = "identity columns require an explicit CACHE (1 or >= 65536)"
)

var (
	identityRe      = regexp.MustCompile(`(?is)\bAS\s+IDENTITY\b(\s*\(([^()]*)\))?`)
	cacheKeywordRe  = regexp.MustCompile(`(?i)\bCACHE\b`)
	cacheOutOfRange = regexp.MustCompile(`(?is)\bCACHE\s+0*(0|[2-9]|[1-9]\d{1,3}|[1-5]\d{4}|6[0-4]\d{3}|65[0-4]\d{2}|655[0-2]\d|6553[0-5])\b`)
)

// identityWithoutCache reports identity columns whose sequence options lack an
// explicit CACHE. Each identity clause is checked independently so one clause
// with CACHE does not hide another without it.
func identityWithoutCache(sql string) string {
	for _, m := range identityRe.FindAllStringSubmatch(sql, -1) {
		if !cacheKeywordRe.MatchString(m[2]) {
			return MsgIdentityCache
		}
	}
	return ""
}

// StatementRules are rules that must be evaluated against a complete SQL
// statement (they may span multiple lines). Patterns anchored with ^ expect
// the statement to have leading whitespace and comments stripped.
var StatementRules = []Rule{
	// Indexes: always asynchronous, btree only, no ASC/DESC (NULLS FIRST/LAST is fine).
	{Name: MsgIndexOrdering, Pattern: regexp.MustCompile(`(?is)^\s*CREATE\s+(UNIQUE\s+)?INDEX\b.*\b(ASC|DESC)\b`)},
	{Name: MsgIndexAsync, Pattern: regexp.MustCompile(`(?is)^\s*CREATE\s+(UNIQUE\s+)?INDEX\b`), Unless: regexp.MustCompile(`(?is)^\s*CREATE\s+(UNIQUE\s+)?INDEX\s+(ASYNC|CONCURRENTLY)\b`)},

	// ALTER TABLE: supported subset (see alter-table-syntax-support).
	{Name: MsgAddConstraintValid, Pattern: regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\b.*\bADD\s+(CONSTRAINT\s+\S+\s+)?(CHECK|FOREIGN\s+KEY)\b`), Unless: regexp.MustCompile(`(?is)\bNOT\s+VALID\b`)},
	{Name: MsgAddPrimaryKey, Pattern: regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\b.*\bADD\s+(CONSTRAINT\s+\S+\s+)?PRIMARY\s+KEY\b`)},
	{Name: MsgAddUnique, Pattern: regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\b.*\bADD\s+(CONSTRAINT\s+\S+\s+)?UNIQUE\b`), Unless: regexp.MustCompile(`(?is)\bUNIQUE\s+USING\s+INDEX\b`)},
	{Name: MsgAlterColumnType, Pattern: regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\b.*\bALTER\s+(COLUMN\s+)?\S+\s+(SET\s+DATA\s+)?TYPE\b`)},
	{Name: MsgSetNotNull, Pattern: regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\b.*\bALTER\s+(COLUMN\s+)?\S+\s+SET\s+NOT\s+NULL\b`)},

	// Sequences and identity columns: bigint only, explicit CACHE of 1 or >= 65536.
	{Name: MsgSequenceCache, Pattern: regexp.MustCompile(`(?is)^\s*CREATE\s+SEQUENCE\b`), Unless: regexp.MustCompile(`(?is)\bCACHE\s+\d+`)},
	{Name: MsgSequenceType, Pattern: regexp.MustCompile(`(?is)^\s*CREATE\s+SEQUENCE\b.*\bAS\s+(smallint|integer|int|int2|int4)\b`)},
	{Name: MsgCacheRange, Pattern: cacheOutOfRange},
	{Name: MsgIdentityType, Pattern: regexp.MustCompile(`(?is)\b(smallint|integer|int|int2|int4)\b[^,]*?\bGENERATED\s+(ALWAYS|BY\s+DEFAULT)\s+AS\s+IDENTITY\b`)},
	{Name: MsgIdentityCache, Func: identityWithoutCache},
}

// Statement is a SQL statement extracted from a script, with comments removed.
type Statement struct {
	Text string // statement text without comments or the trailing semicolon
	Line int    // 1-based line where the statement starts
}

// Split splits a SQL script into statements on top-level semicolons. It
// understands single-quoted strings, double-quoted identifiers, dollar-quoted
// bodies, and -- / block comments (which are removed from the output).
func Split(sql string) []Statement {
	var (
		stmts     []Statement
		buf       strings.Builder
		startLine = 0
		line      = 1
	)

	flush := func() {
		text := strings.TrimSpace(buf.String())
		if text != "" {
			stmts = append(stmts, Statement{Text: text, Line: startLine})
		}
		buf.Reset()
		startLine = 0
	}
	write := func(s string) {
		if startLine == 0 && strings.TrimSpace(s) != "" {
			startLine = line
		}
		buf.WriteString(s)
	}

	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch {
		case ch == '\n':
			buf.WriteByte(ch)
			line++
		case ch == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			i-- // let the newline be handled by the loop
		case ch == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i += 2
			for i < len(sql) && !(sql[i] == '*' && i+1 < len(sql) && sql[i+1] == '/') {
				if sql[i] == '\n' {
					buf.WriteByte('\n')
					line++
				}
				i++
			}
			i++ // skip '/'
			buf.WriteByte(' ')
		case ch == '\'' || ch == '"':
			j := i + 1
			for j < len(sql) {
				if sql[j] == ch {
					if j+1 < len(sql) && sql[j+1] == ch { // escaped quote
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j >= len(sql) {
				j = len(sql) - 1
			}
			lit := sql[i : j+1]
			write(lit)
			line += strings.Count(lit, "\n")
			i = j
		case ch == '$':
			if tag := dollarTag(sql[i:]); tag != "" {
				end := strings.Index(sql[i+len(tag):], tag)
				var lit string
				if end < 0 {
					lit = sql[i:]
				} else {
					lit = sql[i : i+len(tag)+end+len(tag)]
				}
				write(lit)
				line += strings.Count(lit, "\n")
				i += len(lit) - 1
			} else {
				write("$")
			}
		case ch == ';':
			flush()
		default:
			write(string(ch))
		}
	}
	flush()
	return stmts
}

var dollarTagRe = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)?\$`)

func dollarTag(s string) string {
	return dollarTagRe.FindString(s)
}

// StripComments removes -- and /* */ comments from a single SQL statement,
// preserving string literals.
func StripComments(sql string) string {
	var parts []string
	for _, s := range Split(sql) {
		parts = append(parts, s.Text)
	}
	return strings.Join(parts, "; ")
}
