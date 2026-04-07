package schema

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"tomodian/dsql-migrate/internal/ui"

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

	return &IntrospectOutput{Schema: Schema{Tables: tables}}, nil
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

	t.PrimaryKey, t.UniqueConstraints, t.CheckConstraints, err = introspectConstraints(ctx, db, name)
	if err != nil {
		return nil, err
	}

	t.Indexes, err = introspectIndexes(ctx, db, name)
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
		var identityChar string
		if err := rows.Scan(&col.Name, &col.Type, &col.IsNullable, &col.Default, &col.IsIdentity, &identityChar, &col.OrdinalPosition); err != nil {
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

		cols = append(cols, col)
	}
	return cols, rows.Err()
}

func introspectConstraints(ctx context.Context, db *sql.DB, table string) (*PrimaryKey, []UniqueConstraint, []CheckConstraint, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			con.conname,
			con.contype::text,
			pg_get_constraintdef(con.oid)
		FROM pg_constraint con
		WHERE con.conrelid = $1::regclass
		  AND con.contype IN ('p', 'u', 'c')
		ORDER BY con.conname
	`, "public."+table)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("listing constraints for %s: %w", table, err)
	}
	defer rows.Close()

	var pk *PrimaryKey
	var ucs []UniqueConstraint
	var ccs []CheckConstraint

	for rows.Next() {
		var name, contype, def string
		if err := rows.Scan(&name, &contype, &def); err != nil {
			return nil, nil, nil, err
		}

		switch contype {
		case "p":
			cols := parseConstraintDefColumns(def)
			pk = &PrimaryKey{Name: name, Columns: cols}
		case "u":
			cols := parseConstraintDefColumns(def)
			ucs = append(ucs, UniqueConstraint{Name: name, Columns: cols})
		case "c":
			expr := extractCheckExpression(def)
			ccs = append(ccs, CheckConstraint{Name: name, Expression: expr})
		}
	}

	return pk, ucs, ccs, rows.Err()
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
	rows, err := db.QueryContext(ctx, `
		SELECT
			ic.relname,
			i.indisunique
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
		var idx Index
		if err := rows.Scan(&idx.Name, &idx.IsUnique); err != nil {
			return nil, err
		}
		indexes = append(indexes, idx)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	// Fetch columns for each index (requires the connection to be free).
	for i, idx := range indexes {
		cols, err := introspectIndexColumns(ctx, db, table, idx.Name)
		if err != nil {
			return nil, err
		}
		indexes[i].Columns = cols
	}

	return indexes, nil
}

func introspectIndexColumns(ctx context.Context, db *sql.DB, table, indexName string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS x(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = x.attnum
		WHERE ic.relname = $1
		ORDER BY x.ord
	`, indexName)
	if err != nil {
		return nil, fmt.Errorf("listing columns for index %s: %w", indexName, err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		cols = append(cols, strings.ToLower(col))
	}
	return cols, rows.Err()
}
