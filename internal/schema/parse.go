package schema

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"tomodian/dsql-migrate/internal/ui"

	"github.com/go-playground/validator/v10"
)

var parseValidate = validator.New()

// ParseInput is the input for Parse.
type ParseInput struct {
	Files []string `validate:"required,min=1"`
}

// ParseOutput is the output for Parse.
type ParseOutput struct {
	Schema Schema
}

// Parse reads .sql files and parses them into a Schema without executing
// any SQL. Only DSQL-compatible DDL is supported.
func Parse(ctx context.Context, in ParseInput) (*ParseOutput, error) {
	if err := parseValidate.Struct(in); err != nil {
		return nil, fmt.Errorf("invalid parse input: %w", err)
	}

	var stmts []string
	for _, f := range in.Files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f, err)
		}
		for _, s := range splitStatements(string(data)) {
			stmts = append(stmts, s)
		}
		ui.Dim("    Parsed %s\n", f)
	}

	schema, err := parseStatements(stmts)
	if err != nil {
		return nil, err
	}

	return &ParseOutput{Schema: *schema}, nil
}

func splitStatements(sql string) []string {
	var result []string
	for _, part := range strings.Split(sql, ";") {
		s := stripComments(strings.TrimSpace(part))
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

func stripComments(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "--") {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

var (
	createTableRe = regexp.MustCompile(`(?is)^CREATE\s+TABLE\s+(\w+)\s*\((.*)\)\s*$`)
	createIndexRe = regexp.MustCompile(`(?i)^CREATE\s+(UNIQUE\s+)?INDEX\s+ASYNC\s+(\w+)\s+ON\s+(\w+)\s*\(([^)]+)\)$`)
)

func parseStatements(stmts []string) (*Schema, error) {
	tableMap := make(map[string]*Table)
	var tableOrder []string

	for _, stmt := range stmts {
		if m := createTableRe.FindStringSubmatch(stmt); m != nil {
			name := strings.ToLower(m[1])
			body := m[2]
			t, err := parseCreateTable(name, body)
			if err != nil {
				return nil, fmt.Errorf("parsing CREATE TABLE %s: %w", name, err)
			}
			tableMap[name] = t
			tableOrder = append(tableOrder, name)
			continue
		}

		if m := createIndexRe.FindStringSubmatch(stmt); m != nil {
			isUnique := strings.TrimSpace(m[1]) != ""
			idxName := strings.ToLower(m[2])
			tableName := strings.ToLower(m[3])
			cols := parseColumnList(m[4])

			t, ok := tableMap[tableName]
			if !ok {
				return nil, fmt.Errorf("index %s references unknown table %s", idxName, tableName)
			}
			t.Indexes = append(t.Indexes, Index{
				Name:     idxName,
				Columns:  cols,
				IsUnique: isUnique,
			})
			continue
		}

		return nil, fmt.Errorf("unsupported statement: %s", truncate(stmt, 80))
	}

	var tables []Table
	for _, name := range tableOrder {
		tables = append(tables, *tableMap[name])
	}

	return &Schema{Tables: tables}, nil
}

func parseCreateTable(name, body string) (*Table, error) {
	t := &Table{Name: name}

	segments := splitAtTopLevel(body, ',')
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		upper := strings.ToUpper(seg)

		// Table-level PRIMARY KEY
		if strings.HasPrefix(upper, "PRIMARY KEY") {
			cols, err := extractParenColumns(seg)
			if err != nil {
				return nil, fmt.Errorf("parsing PRIMARY KEY: %w", err)
			}
			t.PrimaryKey = &PrimaryKey{
				Name:    name + "_pkey",
				Columns: cols,
			}
			continue
		}

		// Table-level UNIQUE
		if strings.HasPrefix(upper, "UNIQUE") {
			cols, err := extractParenColumns(seg)
			if err != nil {
				return nil, fmt.Errorf("parsing UNIQUE: %w", err)
			}
			t.UniqueConstraints = append(t.UniqueConstraints, UniqueConstraint{
				Name:    name + "_" + strings.Join(cols, "_") + "_key",
				Columns: cols,
			})
			continue
		}

		// Table-level CHECK
		if strings.HasPrefix(upper, "CHECK") {
			expr := extractParenExpr(seg)
			t.CheckConstraints = append(t.CheckConstraints, CheckConstraint{
				Name:       name + "_check",
				Expression: expr,
			})
			continue
		}

		// CONSTRAINT <name> ...
		if strings.HasPrefix(upper, "CONSTRAINT") {
			if err := parseNamedConstraint(t, seg); err != nil {
				return nil, err
			}
			continue
		}

		// Column definition
		col, inlineConstraints, err := parseColumnDef(name, seg)
		if err != nil {
			return nil, fmt.Errorf("parsing column: %w", err)
		}
		t.Columns = append(t.Columns, *col)

		// Apply inline constraints
		for _, ic := range inlineConstraints {
			switch ic.kind {
			case "pk":
				t.PrimaryKey = &PrimaryKey{
					Name:    name + "_pkey",
					Columns: []string{col.Name},
				}
			case "unique":
				t.UniqueConstraints = append(t.UniqueConstraints, UniqueConstraint{
					Name:    name + "_" + col.Name + "_key",
					Columns: []string{col.Name},
				})
			case "check":
				t.CheckConstraints = append(t.CheckConstraints, CheckConstraint{
					Name:       name + "_" + col.Name + "_check",
					Expression: ic.expr,
				})
			}
		}
	}

	return t, nil
}

type inlineConstraint struct {
	kind string // "pk", "unique", "check"
	expr string // for check only
}

func parseColumnDef(tableName, seg string) (*Column, []inlineConstraint, error) {
	tokens := tokenizeColumnDef(seg)
	if len(tokens) < 2 {
		return nil, nil, fmt.Errorf("invalid column definition: %s", seg)
	}

	col := &Column{
		Name:       strings.ToLower(tokens[0]),
		IsNullable: true,
	}

	// Parse type — may be multi-word (e.g., "DOUBLE PRECISION", "TIMESTAMP WITH TIME ZONE")
	typeEnd := 1
	colType := tokens[1]

	// Handle multi-word types
	for typeEnd+1 < len(tokens) {
		next := strings.ToUpper(tokens[typeEnd+1])
		if next == "WITH" || next == "WITHOUT" || next == "PRECISION" || next == "VARYING" || next == "ZONE" || next == "TIME" {
			typeEnd++
			colType += " " + tokens[typeEnd]
		} else {
			break
		}
	}
	// Handle type with parentheses like VARCHAR(255)
	if typeEnd+1 < len(tokens) && strings.HasPrefix(tokens[typeEnd+1], "(") {
		typeEnd++
		colType += tokens[typeEnd]
	}

	col.Type = NormalizeType(colType)

	var constraints []inlineConstraint
	i := typeEnd + 1
	for i < len(tokens) {
		upper := strings.ToUpper(tokens[i])

		switch {
		case upper == "NOT" && i+1 < len(tokens) && strings.ToUpper(tokens[i+1]) == "NULL":
			col.IsNullable = false
			i += 2
		case upper == "NULL":
			col.IsNullable = true
			i++
		case upper == "DEFAULT":
			i++
			def, advance := parseDefaultExpr(tokens, i)
			col.Default = def
			i += advance
		case upper == "PRIMARY" && i+1 < len(tokens) && strings.ToUpper(tokens[i+1]) == "KEY":
			col.IsNullable = false
			constraints = append(constraints, inlineConstraint{kind: "pk"})
			i += 2
		case upper == "UNIQUE":
			constraints = append(constraints, inlineConstraint{kind: "unique"})
			i++
		case upper == "CHECK":
			i++
			expr := extractCheckFromTokens(tokens, i)
			constraints = append(constraints, inlineConstraint{kind: "check", expr: expr})
			// Skip past the check expression
			depth := 0
			for i < len(tokens) {
				for _, ch := range tokens[i] {
					if ch == '(' {
						depth++
					} else if ch == ')' {
						depth--
					}
				}
				i++
				if depth == 0 {
					break
				}
			}
		case upper == "GENERATED":
			gen, advance := parseIdentity(tokens, i)
			if gen != "" {
				col.IsIdentity = true
				col.IdentityGeneration = gen
			}
			i += advance
		default:
			i++
		}
	}

	return col, constraints, nil
}

func parseDefaultExpr(tokens []string, start int) (string, int) {
	if start >= len(tokens) {
		return "", 0
	}

	// Collect tokens until we hit a keyword that ends the default expression
	var parts []string
	depth := 0
	i := start
	for i < len(tokens) {
		upper := strings.ToUpper(tokens[i])
		if depth == 0 && (upper == "NOT" || upper == "NULL" || upper == "PRIMARY" ||
			upper == "UNIQUE" || upper == "CHECK" || upper == "GENERATED" ||
			upper == "CONSTRAINT" || upper == "REFERENCES") {
			break
		}
		for _, ch := range tokens[i] {
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
			}
		}
		parts = append(parts, tokens[i])
		i++
	}

	return strings.Join(parts, " "), i - start
}

func parseIdentity(tokens []string, start int) (string, int) {
	// GENERATED {ALWAYS|BY DEFAULT} AS IDENTITY
	i := start
	if i >= len(tokens) || strings.ToUpper(tokens[i]) != "GENERATED" {
		return "", 1
	}
	i++
	if i >= len(tokens) {
		return "", i - start
	}

	gen := ""
	upper := strings.ToUpper(tokens[i])
	if upper == "ALWAYS" {
		gen = "ALWAYS"
		i++
	} else if upper == "BY" && i+1 < len(tokens) && strings.ToUpper(tokens[i+1]) == "DEFAULT" {
		gen = "BY DEFAULT"
		i += 2
	}

	// Skip "AS IDENTITY"
	if i < len(tokens) && strings.ToUpper(tokens[i]) == "AS" {
		i++
	}
	if i < len(tokens) && strings.ToUpper(tokens[i]) == "IDENTITY" {
		i++
	}

	return gen, i - start
}

func extractCheckFromTokens(tokens []string, start int) string {
	var parts []string
	depth := 0
	for i := start; i < len(tokens); i++ {
		for _, ch := range tokens[i] {
			if ch == '(' {
				depth++
			} else if ch == ')' {
				depth--
			}
		}
		parts = append(parts, tokens[i])
		if depth == 0 && len(parts) > 0 {
			break
		}
	}
	return strings.Join(parts, " ")
}

func parseNamedConstraint(t *Table, seg string) error {
	// CONSTRAINT name {PRIMARY KEY|UNIQUE|CHECK} ...
	parts := strings.Fields(seg)
	if len(parts) < 3 {
		return fmt.Errorf("invalid constraint: %s", seg)
	}
	cname := strings.ToLower(parts[1])
	kind := strings.ToUpper(parts[2])

	switch {
	case kind == "PRIMARY" && len(parts) > 3 && strings.ToUpper(parts[3]) == "KEY":
		cols, err := extractParenColumns(seg)
		if err != nil {
			return err
		}
		t.PrimaryKey = &PrimaryKey{Name: cname, Columns: cols}
	case kind == "UNIQUE":
		cols, err := extractParenColumns(seg)
		if err != nil {
			return err
		}
		t.UniqueConstraints = append(t.UniqueConstraints, UniqueConstraint{Name: cname, Columns: cols})
	case kind == "CHECK":
		expr := extractParenExpr(seg)
		t.CheckConstraints = append(t.CheckConstraints, CheckConstraint{Name: cname, Expression: expr})
	default:
		return fmt.Errorf("unsupported constraint type in: %s", seg)
	}
	return nil
}

// splitAtTopLevel splits a string by separator, but only at parenthesis depth 0.
func splitAtTopLevel(s string, sep rune) []string {
	var result []string
	var current strings.Builder
	depth := 0
	inQuote := false

	for _, ch := range s {
		if ch == '\'' && !inQuote {
			inQuote = true
			current.WriteRune(ch)
			continue
		}
		if ch == '\'' && inQuote {
			inQuote = false
			current.WriteRune(ch)
			continue
		}
		if inQuote {
			current.WriteRune(ch)
			continue
		}

		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
		}

		if ch == sep && depth == 0 {
			result = append(result, current.String())
			current.Reset()
		} else {
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}

func extractParenColumns(s string) ([]string, error) {
	start := strings.Index(s, "(")
	end := strings.LastIndex(s, ")")
	if start < 0 || end < 0 || end <= start {
		return nil, fmt.Errorf("no parenthesized column list in: %s", s)
	}
	return parseColumnList(s[start+1 : end]), nil
}

func extractParenExpr(s string) string {
	start := strings.Index(s, "(")
	end := strings.LastIndex(s, ")")
	if start < 0 || end < 0 || end <= start {
		return s
	}
	return s[start : end+1]
}

func parseColumnList(s string) []string {
	var cols []string
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(strings.ToLower(c))
		if c != "" {
			cols = append(cols, c)
		}
	}
	return cols
}

// tokenizeColumnDef splits a column definition into tokens, preserving
// parenthesized groups as single tokens.
func tokenizeColumnDef(s string) []string {
	var tokens []string
	var current strings.Builder
	depth := 0
	inQuote := false

	for _, ch := range s {
		if ch == '\'' {
			inQuote = !inQuote
			current.WriteRune(ch)
			continue
		}
		if inQuote {
			current.WriteRune(ch)
			continue
		}

		if ch == '(' {
			depth++
			current.WriteRune(ch)
			continue
		}
		if ch == ')' {
			depth--
			current.WriteRune(ch)
			continue
		}

		if depth > 0 {
			current.WriteRune(ch)
			continue
		}

		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
