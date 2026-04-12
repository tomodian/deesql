package proxy

import (
	"regexp"
	"strings"

	"github.com/tomodian/deesql/internal/rules"
)

var (
	// CREATE INDEX without ASYNC: matched separately because Go regexp
	// doesn't support negative lookahead.
	createIndexRe      = regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\s+`)
	createIndexAsyncRe = regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\s+ASYNC\b`)

	// rewriteAsyncRe replaces ASYNC with CONCURRENTLY for the PostgreSQL backend.
	rewriteAsyncRe = regexp.MustCompile(`(?i)(\bCREATE\s+(UNIQUE\s+)?INDEX\s+)ASYNC\s+`)
)

// proxyOnlyBefore are proxy-specific rules that must be checked before shared
// rules (more specific patterns before generic ones).
var proxyOnlyBefore = []rules.Rule{
	{Name: "CREATE MATERIALIZED VIEW or CREATE TABLE AS statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+TABLE\s+\S+\s+AS\s+(SELECT|WITH)\b`)},
	{Name: "CREATE TYPE (enum types) statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+TYPE\s+\S+\s+AS\s+ENUM\b`)},
	{Name: "CREATE TYPE (range types) statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+TYPE\s+\S+\s+AS\s+RANGE\b`)},
}

// proxyOnlyAfter are proxy-specific rules checked after shared rules.
var proxyOnlyAfter = []rules.Rule{
	{Name: "ALTER SYSTEM statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bALTER\s+SYSTEM\b`)},
	{Name: "VACUUM statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bVACUUM\b`)},
	{Name: "CREATE TABLE COLLATE is unsupported", Pattern: regexp.MustCompile(`(?i)\bCOLLATE\s+\S+`)},
	{Name: "CREATE Index with ordering ASC or DESC is unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\b.*\b(ASC|DESC)\b`)},

	// CREATE PROCEDURE / RULE / UNLOGGED TABLE
	{Name: "CREATE PROCEDURE statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?PROCEDURE\b`)},
	{Name: "CREATE RULE statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+(OR\s+REPLACE\s+)?RULE\b`)},
	{Name: "CREATE UNLOGGED TABLE is unsupported", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+UNLOGGED\s+TABLE\b`)},

	// Non-btree index types
	{Name: "only btree indexes supported (hash not supported)", Pattern: regexp.MustCompile(`(?i)\bUSING\s+hash\b`)},
	{Name: "only btree indexes supported (gin not supported)", Pattern: regexp.MustCompile(`(?i)\bUSING\s+gin\b`)},
	{Name: "only btree indexes supported (gist not supported)", Pattern: regexp.MustCompile(`(?i)\bUSING\s+gist\b`)},
	{Name: "only btree indexes supported (brin not supported)", Pattern: regexp.MustCompile(`(?i)\bUSING\s+brin\b`)},
	{Name: "only btree indexes supported (spgist not supported)", Pattern: regexp.MustCompile(`(?i)\bUSING\s+spgist\b`)},

	// CREATE INDEX CONCURRENTLY (must use ASYNC)
	{Name: "CREATE INDEX CONCURRENTLY not supported (use CREATE INDEX ASYNC)", Pattern: regexp.MustCompile(`(?i)\bCREATE\s+(UNIQUE\s+)?INDEX\s+CONCURRENTLY\b`)},

	// Unsupported constraints
	{Name: "FOREIGN KEY constraint not supported", Pattern: regexp.MustCompile(`(?i)\bFOREIGN\s+KEY\b`)},
	{Name: "FOREIGN KEY constraint not supported", Pattern: regexp.MustCompile(`(?i)\bREFERENCES\s+\w+`)},
	{Name: "EXCLUDE constraint not supported", Pattern: regexp.MustCompile(`(?i)\bEXCLUDE\s+(USING\s+)?\(`)},

	// Savepoints (DSQL does not support savepoints)
	{Name: "RELEASE SAVEPOINT statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bRELEASE\s+SAVEPOINT\b`)},
	{Name: "SAVEPOINT statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bSAVEPOINT\b`)},

	// LISTEN/NOTIFY (DSQL does not support async notifications)
	{Name: "LISTEN statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bLISTEN\b`)},
	{Name: "NOTIFY statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bNOTIFY\b`)},
	{Name: "UNLISTEN statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bUNLISTEN\b`)},

	// Explicit locking (DSQL uses OCC, no pessimistic locking)
	{Name: "LOCK TABLE statements are unsupported", Pattern: regexp.MustCompile(`(?i)\bLOCK\s+TABLE\b`)},

	// SET session parameters (DSQL does not support most SET commands)
	{Name: "SET statement_timeout is unsupported", Pattern: regexp.MustCompile(`(?i)\bSET\s+(LOCAL\s+)?statement_timeout\b`)},
	{Name: "SET lock_timeout is unsupported", Pattern: regexp.MustCompile(`(?i)\bSET\s+(LOCAL\s+)?lock_timeout\b`)},
	{Name: "SET idle_in_transaction_session_timeout is unsupported", Pattern: regexp.MustCompile(`(?i)\bSET\s+(LOCAL\s+)?idle_in_transaction_session_timeout\b`)},

	// Isolation level (only REPEATABLE READ supported)
	{Name: "only ISOLATION LEVEL REPEATABLE READ is supported", Pattern: regexp.MustCompile(`(?i)\bISOLATION\s+LEVEL\s+(READ\s+COMMITTED|READ\s+UNCOMMITTED|SERIALIZABLE)\b`)},

	// ANALYZE without table name
	{Name: "ANALYZE requires a table name", Pattern: regexp.MustCompile(`(?i)^\s*ANALYZE\s*$`)},
}

// allProxyRules combines proxy-specific (before) + shared + proxy-specific (after)
// to ensure more specific patterns are checked before generic ones.
var allProxyRules = func() []rules.Rule {
	all := make([]rules.Rule, 0, len(proxyOnlyBefore)+len(rules.SharedRules)+len(proxyOnlyAfter))
	all = append(all, proxyOnlyBefore...)
	all = append(all, rules.SharedRules...)
	all = append(all, proxyOnlyAfter...)
	return all
}()

// Check inspects a SQL string and returns an error message if it uses a
// DSQL-unsupported feature, or "" if the statement is allowed.
func Check(sql string) string {
	for _, stmt := range splitStatements(sql) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		for _, r := range allProxyRules {
			if msg := r.Match(stmt); msg != "" {
				return msg
			}
		}
		// CREATE INDEX without ASYNC (separate check — Go regexp lacks negative lookahead).
		if createIndexRe.MatchString(stmt) && !createIndexAsyncRe.MatchString(stmt) {
			return "CREATE INDEX must use ASYNC (use CREATE INDEX ASYNC)"
		}
	}
	return ""
}

// Rewrite converts DSQL-specific SQL to PostgreSQL-compatible SQL.
// CREATE [UNIQUE] INDEX ASYNC → CREATE [UNIQUE] INDEX CONCURRENTLY.
func Rewrite(sql string) string {
	return rewriteAsyncRe.ReplaceAllString(sql, "${1}CONCURRENTLY ")
}

// TxState tracks transaction state for DDL/DML enforcement warnings.
type TxState struct {
	InTx   bool
	HasDDL bool
	HasDML bool
}

var (
	beginRe  = regexp.MustCompile(`(?i)^\s*(BEGIN|START\s+TRANSACTION)\b`)
	commitRe = regexp.MustCompile(`(?i)^\s*(COMMIT|ROLLBACK|END|ABORT)\b`)
	ddlRe    = regexp.MustCompile(`(?i)^\s*(CREATE|ALTER|DROP)\b`)
	dmlRe    = regexp.MustCompile(`(?i)^\s*(INSERT|UPDATE|DELETE)\b`)
)

// TrackTx updates transaction state and returns a warning message if the
// statement would violate DSQL's transaction rules, or "" if okay.
func (tx *TxState) TrackTx(sql string) string {
	stmt := strings.TrimSpace(sql)
	if stmt == "" {
		return ""
	}

	if beginRe.MatchString(stmt) {
		tx.InTx = true
		tx.HasDDL = false
		tx.HasDML = false
		return ""
	}

	if commitRe.MatchString(stmt) {
		tx.InTx = false
		tx.HasDDL = false
		tx.HasDML = false
		return ""
	}

	if !tx.InTx {
		return ""
	}

	if ddlRe.MatchString(stmt) {
		if tx.HasDML {
			tx.HasDDL = true
			return "mixed DDL and DML in one transaction — would fail on Aurora DSQL"
		}
		if tx.HasDDL {
			return "multiple DDL statements in one transaction — would fail on Aurora DSQL"
		}
		tx.HasDDL = true
		return ""
	}

	if dmlRe.MatchString(stmt) {
		if tx.HasDDL {
			tx.HasDML = true
			return "mixed DDL and DML in one transaction — would fail on Aurora DSQL"
		}
		tx.HasDML = true
		return ""
	}

	return ""
}

// splitStatements splits a SQL string on semicolons, respecting single-quoted
// string literals (so semicolons inside strings are not treated as delimiters).
func splitStatements(sql string) []string {
	var stmts []string
	var buf strings.Builder
	inQuote := false

	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch {
		case ch == '\'' && !inQuote:
			inQuote = true
			buf.WriteByte(ch)
		case ch == '\'' && inQuote:
			// Handle escaped quotes ('')
			if i+1 < len(sql) && sql[i+1] == '\'' {
				buf.WriteByte(ch)
				buf.WriteByte(ch)
				i++
			} else {
				inQuote = false
				buf.WriteByte(ch)
			}
		case ch == ';' && !inQuote:
			stmts = append(stmts, buf.String())
			buf.Reset()
		default:
			buf.WriteByte(ch)
		}
	}

	if s := buf.String(); strings.TrimSpace(s) != "" {
		stmts = append(stmts, s)
	}

	return stmts
}
