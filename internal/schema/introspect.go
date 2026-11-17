package schema

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/tomodian/deesql/internal/ui"

	"github.com/go-playground/validator/v10"
)

var introspectValidate = validator.New()

// IntrospectInput is the input for Introspect.
type IntrospectInput struct {
	DB *sql.DB `validate:"required"`
}

// IntrospectOutput is the output for Introspect.
type IntrospectOutput struct {
	Schema Schema
}

// Introspect reads the public schema of a DSQL database via pg_catalog.
func Introspect(ctx context.Context, in IntrospectInput) (*IntrospectOutput, error) {
	if err := introspectValidate.Struct(in); err != nil {
		return nil, fmt.Errorf("invalid introspect input: %w", err)
	}

	ui.Step("Introspecting live schema...")

	tableNames, err := listTables(ctx, in.DB)
	if err != nil {
		return nil, err
	}
	ui.Dim("    Found %d table(s)\n", len(tableNames))

	var tables []Table
	for _, name := range tableNames {
		t, err := introspectTable(ctx, in.DB, name)
		if err != nil {
			return nil, fmt.Errorf("introspecting table %s: %w", name, err)
		}
		tables = append(tables, *t)
	}

	sequences, err := introspectSequences(ctx, in.DB)
	if err != nil {
		return nil, err
	}

	return &IntrospectOutput{Schema: Schema{Tables: tables, Sequences: sequences}}, nil
}

func listTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		ORDER BY c.relname
	`)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func introspectTable(ctx context.Context, db *sql.DB, name string) (*Table, error) {
	t := &Table{Name: name}

	var err error
	t.Columns, err = introspectColumns(ctx, db, name)
	if err != nil {
		return nil, err
	}

	if err := introspectConstraints(ctx, db, t); err != nil {
		return nil, err
	}

	t.Indexes, err = introspectIndexes(ctx, db, name)
	if err != nil {
		return nil, err
	}

	t.Statistics, err = introspectStatistics(ctx, db, name)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func introspectColumns(ctx context.Context, db *sql.DB, table string) ([]Column, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			a.attname,
			format_type(a.atttypid, a.atttypmod),
			NOT a.attnotnull,
			COALESCE(pg_get_expr(d.adbin, d.adrelid), ''),
			a.attidentity != '',
			a.attidentity,
			a.attgenerated::text,
			a.attnum
		FROM pg_attribute a
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attrelid = $1::regclass
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		ORDER BY a.attnum
	`, "public."+table)
	if err != nil {
		return nil, fmt.Errorf("listing columns for %s: %w", table, err)
	}
	defer rows.Close()

	var cols []Column
	for rows.Next() {
		var col Column
		var identityChar, generatedChar string
		if err := rows.Scan(&col.Name, &col.Type, &col.IsNullable, &col.Default, &col.IsIdentity, &identityChar, &generatedChar, &col.OrdinalPosition); err != nil {
			return nil, err
		}
		col.Type = NormalizeType(col.Type)
		col.Name = strings.ToLower(col.Name)

		switch identityChar {
		case "a":
			col.IdentityGeneration = "ALWAYS"
		case "d":
			col.IdentityGeneration = "BY DEFAULT"
		}

		// Strip identity-generated defaults (nextval) — the identity flag is sufficient.
		if col.IsIdentity {
			col.Default = ""
		}

		// For stored generated columns pg_attrdef holds the generation expression.
		if generatedChar == "s" {
			col.Generated = col.Default
			col.Default = ""
		}

		cols = append(cols, col)
	}
	return cols, rows.Err()
}

func introspectConstraints(ctx context.Context, db *sql.DB, t *Table) error {
	rows, err := db.QueryContext(ctx, `
		SELECT
			con.conname,
			con.contype::text,
			pg_get_constraintdef(con.oid)
		FROM pg_constraint con
		WHERE con.conrelid = $1::regclass
		  AND con.contype IN ('p', 'u', 'c', 'f')
		ORDER BY con.conname
	`, "public."+t.Name)
	if err != nil {
		return fmt.Errorf("listing constraints for %s: %w", t.Name, err)
	}
	defer rows.Close()

	for rows.Next() {
		var name, contype, def string
		if err := rows.Scan(&name, &contype, &def); err != nil {
			return err
		}
		def, notValid := stripNotValid(def)

		switch contype {
		case "p":
			t.PrimaryKey = &PrimaryKey{Name: name, Columns: parseConstraintDefColumns(def)}
		case "u":
			t.UniqueConstraints = append(t.UniqueConstraints, UniqueConstraint{
				Name:             name,
				Columns:          parseConstraintDefColumns(def),
				NullsNotDistinct: nullsNotDistinctRe.MatchString(def),
			})
		case "c":
			t.CheckConstraints = append(t.CheckConstraints, CheckConstraint{
				Name:       name,
				Expression: extractCheckExpression(def),
				NotValid:   notValid,
			})
		case "f":
			fk, err := parseForeignKeyDef(def)
			if err != nil {
				return fmt.Errorf("constraint %s.%s: %w", t.Name, name, err)
			}
			fk.Name = name
			fk.NotValid = notValid
			t.ForeignKeys = append(t.ForeignKeys, *fk)
		}
	}

	return rows.Err()
}

var notValidSuffixRe = regexp.MustCompile(`(?i)\s+NOT\s+VALID\s*$`)

// stripNotValid removes a trailing "NOT VALID" from a constraint definition.
func stripNotValid(def string) (string, bool) {
	if loc := notValidSuffixRe.FindStringIndex(def); loc != nil {
		return def[:loc[0]], true
	}
	return def, false
}

// parseConstraintDefColumns extracts column names from constraint def like
// "PRIMARY KEY (id) INCLUDE (...)". DSQL appends INCLUDE which we strip.
func parseConstraintDefColumns(def string) []string {
	upper := strings.ToUpper(def)
	if idx := strings.Index(upper, " INCLUDE "); idx >= 0 {
		def = def[:idx]
	}
	start := strings.Index(def, "(")
	end := strings.LastIndex(def, ")")
	if start < 0 || end < 0 {
		return nil
	}
	return parseColumnList(def[start+1 : end])
}

// extractCheckExpression extracts the expression from "CHECK ((expr))".
func extractCheckExpression(def string) string {
	prefix := "CHECK "
	upper := strings.ToUpper(def)
	if strings.HasPrefix(upper, prefix) {
		return def[len(prefix):]
	}
	return def
}

func introspectIndexes(ctx context.Context, db *sql.DB, table string) ([]Index, error) {
	// Constraint-backed indexes (PK, UNIQUE) are managed via constraints.
	// Invalid indexes (still building or failed) are skipped.
	rows, err := db.QueryContext(ctx, `
		SELECT
			ic.relname,
			pg_get_indexdef(i.indexrelid)
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_class tc ON tc.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = tc.relnamespace
		WHERE n.nspname = 'public'
		  AND tc.relname = $1
		  AND i.indisvalid
		  AND NOT EXISTS (
			  SELECT 1 FROM pg_constraint c
			  WHERE c.conindid = i.indexrelid
		  )
		ORDER BY ic.relname
	`, table)
	if err != nil {
		return nil, fmt.Errorf("listing indexes for %s: %w", table, err)
	}
	defer rows.Close()

	var indexes []Index
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			return nil, err
		}
		_, idx, err := parseIndexDef(def, false)
		if err != nil {
			return nil, fmt.Errorf("parsing definition of index %s: %w", name, err)
		}
		idx.Name = strings.ToLower(name)
		indexes = append(indexes, *idx)
	}
	return indexes, rows.Err()
}

func introspectStatistics(ctx context.Context, db *sql.DB, table string) ([]Statistics, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			s.stxname,
			pg_get_statisticsobjdef(s.oid)
		FROM pg_statistic_ext s
		WHERE s.stxrelid = $1::regclass
		ORDER BY s.stxname
	`, "public."+table)
	if err != nil {
		return nil, fmt.Errorf("listing statistics for %s: %w", table, err)
	}
	defer rows.Close()

	var stats []Statistics
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			return nil, err
		}
		_, st, err := parseStatisticsDef(def)
		if err != nil {
			return nil, fmt.Errorf("parsing definition of statistics %s: %w", name, err)
		}
		st.Name = strings.ToLower(name)
		stats = append(stats, *st)
	}
	return stats, rows.Err()
}

func introspectSequences(ctx context.Context, db *sql.DB) ([]Sequence, error) {
	// Sequences backing identity columns (deptype 'i') are managed through
	// the column definition and excluded here.
	rows, err := db.QueryContext(ctx, `
		SELECT
			c.relname,
			s.seqincrement,
			s.seqmin,
			s.seqmax,
			s.seqstart,
			s.seqcache,
			s.seqcycle
		FROM pg_sequence s
		JOIN pg_class c ON c.oid = s.seqrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND NOT EXISTS (
			  SELECT 1 FROM pg_depend d
			  WHERE d.objid = c.oid AND d.deptype = 'i'
		  )
		ORDER BY c.relname
	`)
	if err != nil {
		return nil, fmt.Errorf("listing sequences: %w", err)
	}
	defer rows.Close()

	var seqs []Sequence
	for rows.Next() {
		var seq Sequence
		if err := rows.Scan(&seq.Name, &seq.Increment, &seq.MinValue, &seq.MaxValue, &seq.Start, &seq.Cache, &seq.Cycle); err != nil {
			return nil, err
		}
		seq.Name = strings.ToLower(seq.Name)
		seqs = append(seqs, seq)
	}
	return seqs, rows.Err()
}
