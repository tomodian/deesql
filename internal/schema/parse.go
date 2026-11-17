package schema

import (
	"context"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tomodian/deesql/internal/rules"
	"github.com/tomodian/deesql/internal/ui"

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
		stmts = append(stmts, splitStatements(string(data))...)
		ui.Dim("    Parsed %s\n", f)
	}

	schema, err := parseStatements(stmts)
	if err != nil {
		return nil, err
	}

	return &ParseOutput{Schema: *schema}, nil
}

// splitStatements splits a SQL script into statements, removing comments and
// respecting quoted strings and dollar-quoted bodies.
func splitStatements(sql string) []string {
	var result []string
	for _, s := range rules.Split(sql) {
		result = append(result, s.Text)
	}
	return result
}

var (
	createTableRe = regexp.MustCompile(`(?is)^CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?((?:\w+\.)?\w+)\s*\((.*)\)\s*$`)
	createIndexRe = regexp.MustCompile(`(?is)^CREATE\s+(?:UNIQUE\s+)?INDEX\b`)
	createSeqRe   = regexp.MustCompile(`(?is)^CREATE\s+SEQUENCE\b`)
	createStatsRe = regexp.MustCompile(`(?is)^CREATE\s+STATISTICS\b`)

	// Statements the parser recognizes as valid DSQL SQL but does not manage
	// (not part of table/index/sequence/statistics diffing). These are skipped.
	skipRe = regexp.MustCompile(`(?is)^(?:` +
		`CREATE\s+(?:OR\s+REPLACE\s+)?(?:VIEW|RECURSIVE\s+VIEW|FUNCTION|DOMAIN|ROLE|USER|SCHEMA)|` +
		`ALTER\s+(?:VIEW|SEQUENCE|ROLE|USER|GROUP|FUNCTION|ROUTINE|DOMAIN|SCHEMA|SYSTEM|STATISTICS|DEFAULT\s+PRIVILEGES)|` +
		`DROP\s+(?:VIEW|FUNCTION|ROUTINE|SEQUENCE|DOMAIN|ROLE|USER|SCHEMA|STATISTICS)|` +
		`COMMENT\s+ON\b|GRANT\b|REVOKE\b)`)
)

func parseStatements(stmts []string) (*Schema, error) {
	tableMap := make(map[string]*Table)
	var tableOrder []string

	type pendingIndex struct {
		table string
		index Index
	}
	type pendingStats struct {
		table string
		stats Statistics
	}
	var indexes []pendingIndex
	var stats []pendingStats
	var sequences []Sequence

	for _, stmt := range stmts {
		if m := createTableRe.FindStringSubmatch(stmt); m != nil {
			name := unqualify(m[1])
			t, err := parseCreateTable(name, m[2])
			if err != nil {
				return nil, fmt.Errorf("parsing CREATE TABLE %s: %w", name, err)
			}
			if _, dup := tableMap[name]; dup {
				return nil, fmt.Errorf("table %s is defined more than once", name)
			}
			tableMap[name] = t
			tableOrder = append(tableOrder, name)
			continue
		}

		if createIndexRe.MatchString(stmt) {
			table, idx, err := parseIndexDef(stmt, true)
			if err != nil {
				return nil, err
			}
			indexes = append(indexes, pendingIndex{table: table, index: *idx})
			continue
		}

		if createSeqRe.MatchString(stmt) {
			seq, err := parseSequence(stmt)
			if err != nil {
				return nil, err
			}
			sequences = append(sequences, *seq)
			continue
		}

		if createStatsRe.MatchString(stmt) {
			table, st, err := parseStatisticsDef(stmt)
			if err != nil {
				return nil, err
			}
			stats = append(stats, pendingStats{table: table, stats: *st})
			continue
		}

		// Skip statements the parser doesn't manage (views, functions, etc.).
		if skipRe.MatchString(stmt) {
			continue
		}

		return nil, fmt.Errorf("unsupported statement: %s", truncate(stmt, 80))
	}

	for _, pi := range indexes {
		t, ok := tableMap[pi.table]
		if !ok {
			return nil, fmt.Errorf("index %s references unknown table %s", pi.index.Name, pi.table)
		}
		t.Indexes = append(t.Indexes, pi.index)
	}
	for _, ps := range stats {
		t, ok := tableMap[ps.table]
		if !ok {
			return nil, fmt.Errorf("statistics %s references unknown table %s", ps.stats.Name, ps.table)
		}
		t.Statistics = append(t.Statistics, ps.stats)
	}

	// Resolve foreign keys: default referenced columns to the referenced
	// table's primary key, and make sure the referenced table is managed.
	for _, name := range tableOrder {
		t := tableMap[name]
		for i := range t.ForeignKeys {
			fk := &t.ForeignKeys[i]
			ref, ok := tableMap[fk.RefTable]
			if !ok {
				return nil, fmt.Errorf("foreign key %s.%s references unknown table %s", t.Name, fk.Name, fk.RefTable)
			}
			if len(fk.RefColumns) == 0 {
				if ref.PrimaryKey == nil {
					return nil, fmt.Errorf("foreign key %s.%s references table %s which has no primary key", t.Name, fk.Name, fk.RefTable)
				}
				fk.RefColumns = append([]string(nil), ref.PrimaryKey.Columns...)
			}
		}
	}

	var tables []Table
	for _, name := range tableOrder {
		tables = append(tables, *tableMap[name])
	}

	return &Schema{Tables: tables, Sequences: sequences}, nil
}

func parseCreateTable(name, body string) (*Table, error) {
	t := &Table{Name: name}

	for _, seg := range splitAtTopLevel(body, ',') {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		upper := strings.ToUpper(seg)

		if strings.HasPrefix(upper, "CONSTRAINT") {
			parts := strings.Fields(seg)
			if len(parts) < 3 {
				return nil, fmt.Errorf("invalid constraint: %s", seg)
			}
			cname := strings.ToLower(parts[1])
			rest := strings.TrimSpace(seg[strings.Index(seg, parts[1])+len(parts[1]):])
			ok, err := parseTableConstraint(t, cname, rest)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("unsupported constraint type in: %s", seg)
			}
			continue
		}

		if ok, err := parseTableConstraint(t, "", seg); err != nil {
			return nil, err
		} else if ok {
			continue
		}

		// Column definition
		col, inlineConstraints, err := parseColumnDef(seg)
		if err != nil {
			return nil, fmt.Errorf("parsing column: %w", err)
		}
		t.Columns = append(t.Columns, *col)

		for _, ic := range inlineConstraints {
			switch ic.kind {
			case "pk":
				t.PrimaryKey = &PrimaryKey{
					Name:    orDefault(ic.name, name+"_pkey"),
					Columns: []string{col.Name},
				}
			case "unique":
				t.UniqueConstraints = append(t.UniqueConstraints, UniqueConstraint{
					Name:             orDefault(ic.name, name+"_"+col.Name+"_key"),
					Columns:          []string{col.Name},
					NullsNotDistinct: ic.nullsNotDistinct,
				})
			case "check":
				t.CheckConstraints = append(t.CheckConstraints, CheckConstraint{
					Name:       orDefault(ic.name, name+"_"+col.Name+"_check"),
					Expression: ic.expr,
				})
			case "fk":
				fk := *ic.fk
				fk.Columns = []string{col.Name}
				fk.Name = orDefault(ic.name, name+"_"+col.Name+"_fkey")
				t.ForeignKeys = append(t.ForeignKeys, fk)
			}
		}
	}

	// Primary key and identity columns are implicitly NOT NULL.
	pkCols := map[string]bool{}
	if t.PrimaryKey != nil {
		for _, c := range t.PrimaryKey.Columns {
			pkCols[c] = true
		}
	}
	for i := range t.Columns {
		if pkCols[t.Columns[i].Name] || t.Columns[i].IsIdentity {
			t.Columns[i].IsNullable = false
		}
	}

	dedupeConstraintNames(t)
	return t, nil
}

// parseTableConstraint parses a table-level constraint (without the leading
// "CONSTRAINT name"). It returns false if seg is not a table constraint.
func parseTableConstraint(t *Table, cname, seg string) (bool, error) {
	upper := strings.ToUpper(seg)
	switch {
	case strings.HasPrefix(upper, "PRIMARY KEY"):
		cols, err := extractParenColumns(seg)
		if err != nil {
			return true, fmt.Errorf("parsing PRIMARY KEY: %w", err)
		}
		t.PrimaryKey = &PrimaryKey{Name: orDefault(cname, t.Name+"_pkey"), Columns: cols}
		return true, nil

	case strings.HasPrefix(upper, "UNIQUE"):
		cols, err := extractParenColumns(seg)
		if err != nil {
			return true, fmt.Errorf("parsing UNIQUE: %w", err)
		}
		t.UniqueConstraints = append(t.UniqueConstraints, UniqueConstraint{
			Name:             orDefault(cname, t.Name+"_"+strings.Join(cols, "_")+"_key"),
			Columns:          cols,
			NullsNotDistinct: nullsNotDistinctRe.MatchString(seg),
		})
		return true, nil

	case strings.HasPrefix(upper, "CHECK"):
		t.CheckConstraints = append(t.CheckConstraints, CheckConstraint{
			Name:       orDefault(cname, t.Name+"_check"),
			Expression: extractParenExpr(seg),
		})
		return true, nil

	case strings.HasPrefix(upper, "FOREIGN"):
		fk, err := parseForeignKeyDef(seg)
		if err != nil {
			return true, err
		}
		fk.Name = orDefault(cname, t.Name+"_"+strings.Join(fk.Columns, "_")+"_fkey")
		t.ForeignKeys = append(t.ForeignKeys, *fk)
		return true, nil
	}

	return false, nil
}

var nullsNotDistinctRe = regexp.MustCompile(`(?i)\bNULLS\s+NOT\s+DISTINCT\b`)

// dedupeConstraintNames appends numeric suffixes to auto-generated constraint
// names that collide, mirroring PostgreSQL's naming.
func dedupeConstraintNames(t *Table) {
	used := map[string]bool{}
	if t.PrimaryKey != nil {
		used[t.PrimaryKey.Name] = true
	}
	unique := func(name string) string {
		if !used[name] {
			used[name] = true
			return name
		}
		for i := 1; ; i++ {
			candidate := fmt.Sprintf("%s%d", name, i)
			if !used[candidate] {
				used[candidate] = true
				return candidate
			}
		}
	}
	for i := range t.UniqueConstraints {
		t.UniqueConstraints[i].Name = unique(t.UniqueConstraints[i].Name)
	}
	for i := range t.CheckConstraints {
		t.CheckConstraints[i].Name = unique(t.CheckConstraints[i].Name)
	}
	for i := range t.ForeignKeys {
		t.ForeignKeys[i].Name = unique(t.ForeignKeys[i].Name)
	}
}

type inlineConstraint struct {
	kind             string // "pk", "unique", "check", "fk"
	name             string // explicit CONSTRAINT name, empty if unnamed
	expr             string // for check only
	nullsNotDistinct bool   // for unique only
	fk               *ForeignKey
}

var multiWordTypeTokens = map[string]bool{
	"WITH": true, "WITHOUT": true, "PRECISION": true, "VARYING": true, "ZONE": true, "TIME": true,
}

func parseColumnDef(seg string) (*Column, []inlineConstraint, error) {
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
	for typeEnd+1 < len(tokens) && multiWordTypeTokens[strings.ToUpper(tokens[typeEnd+1])] {
		typeEnd++
		colType += " " + tokens[typeEnd]
	}
	// Handle type with parentheses like VARCHAR (255)
	if typeEnd+1 < len(tokens) && strings.HasPrefix(tokens[typeEnd+1], "(") {
		typeEnd++
		colType += tokens[typeEnd]
	}
	col.Type = NormalizeType(colType)

	var constraints []inlineConstraint
	pendingName := ""
	add := func(ic inlineConstraint) {
		ic.name = pendingName
		pendingName = ""
		constraints = append(constraints, ic)
	}

	i := typeEnd + 1
	for i < len(tokens) {
		upper := strings.ToUpper(tokens[i])

		switch {
		case upper == "CONSTRAINT" && i+1 < len(tokens):
			pendingName = strings.ToLower(tokens[i+1])
			i += 2
		case upper == "NOT" && i+1 < len(tokens) && strings.ToUpper(tokens[i+1]) == "NULL":
			col.IsNullable = false
			pendingName = ""
			i += 2
		case upper == "NULL":
			col.IsNullable = true
			pendingName = ""
			i++
		case upper == "DEFAULT":
			i++
			def, advance := parseDefaultExpr(tokens, i)
			col.Default = def
			pendingName = ""
			i += advance
		case upper == "PRIMARY" && i+1 < len(tokens) && strings.ToUpper(tokens[i+1]) == "KEY":
			col.IsNullable = false
			add(inlineConstraint{kind: "pk"})
			i += 2
		case upper == "UNIQUE":
			i++
			nnd := false
			if i+2 < len(tokens) && strings.ToUpper(tokens[i]) == "NULLS" && strings.ToUpper(tokens[i+1]) == "NOT" && strings.ToUpper(tokens[i+2]) == "DISTINCT" {
				nnd = true
				i += 3
			} else if i+1 < len(tokens) && strings.ToUpper(tokens[i]) == "NULLS" && strings.ToUpper(tokens[i+1]) == "DISTINCT" {
				i += 2
			}
			add(inlineConstraint{kind: "unique", nullsNotDistinct: nnd})
		case upper == "CHECK" || strings.HasPrefix(upper, "CHECK("):
			if upper != "CHECK" {
				// CHECK(expr) written without a space.
				tokens = append(tokens[:i+1], append([]string{tokens[i][len("CHECK"):]}, tokens[i+1:]...)...)
			}
			i++
			expr := extractCheckFromTokens(tokens, i)
			add(inlineConstraint{kind: "check", expr: expr})
			// Skip past the check expression
			depth := 0
			for i < len(tokens) {
				depth += parenDelta(tokens[i])
				i++
				if depth == 0 {
					break
				}
			}
		case upper == "REFERENCES" || strings.HasPrefix(upper, "REFERENCES("):
			fk, advance, err := parseReferences(tokens, i)
			if err != nil {
				return nil, nil, err
			}
			add(inlineConstraint{kind: "fk", fk: fk})
			i += advance
		case upper == "GENERATED":
			advance := parseGenerated(col, tokens, i)
			pendingName = ""
			i += advance
		case upper == "COLLATE" || upper == "STORAGE" || upper == "COMPRESSION":
			i += 2
		default:
			i++
		}
	}

	if col.IsIdentity {
		col.IsNullable = false
	}

	return col, constraints, nil
}

func parenDelta(s string) int {
	d := 0
	for _, ch := range s {
		if ch == '(' {
			d++
		} else if ch == ')' {
			d--
		}
	}
	return d
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
			upper == "CONSTRAINT" || upper == "REFERENCES" || upper == "COLLATE") {
			break
		}
		depth += parenDelta(tokens[i])
		parts = append(parts, tokens[i])
		i++
	}

	return strings.Join(parts, " "), i - start
}

// parseGenerated handles:
//
//	GENERATED {ALWAYS|BY DEFAULT} AS IDENTITY [ ( sequence_options ) ]
//	GENERATED ALWAYS AS ( expr ) STORED
//
// and returns the number of tokens consumed.
func parseGenerated(col *Column, tokens []string, start int) int {
	i := start + 1
	if i >= len(tokens) {
		return i - start
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

	if i < len(tokens) && strings.ToUpper(tokens[i]) == "AS" {
		i++
	}
	if i >= len(tokens) {
		return i - start
	}

	tok := tokens[i]
	switch {
	case strings.HasPrefix(strings.ToUpper(tok), "IDENTITY"):
		col.IsIdentity = true
		col.IdentityGeneration = gen
		rest := strings.TrimSpace(tok[len("IDENTITY"):])
		i++
		if rest == "" && i < len(tokens) && strings.HasPrefix(tokens[i], "(") {
			rest = tokens[i]
			i++
		}
		if rest != "" {
			col.IdentityOptions = strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimPrefix(rest, "("), ")")), " ")
		}
	case strings.HasPrefix(tok, "("):
		col.Generated = tok
		i++
		if i < len(tokens) && strings.ToUpper(tokens[i]) == "STORED" {
			i++
		}
	}

	return i - start
}

// parseReferences parses a REFERENCES clause starting at tokens[start] and
// returns the foreign key (without Name/Columns) and the tokens consumed.
func parseReferences(tokens []string, start int) (*ForeignKey, int, error) {
	// Normalize "REFERENCES(" and "table(cols)" into separate tokens.
	var toks []string
	for _, tok := range tokens[start:] {
		head, paren := splitHeadParen(tok)
		if head != "" {
			toks = append(toks, head)
		}
		if paren != "" {
			toks = append(toks, paren)
		}
	}
	// Map positions in toks back to positions in tokens.
	consumedOrig := func(n int) int {
		count := 0
		for j, tok := range tokens[start:] {
			head, paren := splitHeadParen(tok)
			if head != "" {
				count++
			}
			if paren != "" {
				count++
			}
			if count >= n {
				return j + 1
			}
		}
		return len(tokens) - start
	}

	i := 1 // skip REFERENCES
	if i >= len(toks) {
		return nil, 0, fmt.Errorf("REFERENCES without a table")
	}
	fk := &ForeignKey{RefTable: unqualify(toks[i])}
	i++
	if i < len(toks) && strings.HasPrefix(toks[i], "(") {
		fk.RefColumns = parseColumnList(strings.TrimSuffix(strings.TrimPrefix(toks[i], "("), ")"))
		i++
	}

	upperAt := func(j int) string {
		if j < len(toks) {
			return strings.ToUpper(toks[j])
		}
		return ""
	}

	for i < len(toks) {
		switch {
		case upperAt(i) == "MATCH":
			fk.MatchFull = upperAt(i+1) == "FULL"
			i += 2
		case upperAt(i) == "ON" && (upperAt(i+1) == "DELETE" || upperAt(i+1) == "UPDATE"):
			event := upperAt(i + 1)
			i += 2
			var action string
			switch upperAt(i) {
			case "NO":
				action = "" // NO ACTION
				i += 2
			case "RESTRICT", "CASCADE":
				action = upperAt(i)
				i++
			case "SET":
				action = "SET " + upperAt(i+1)
				i += 2
				if i < len(toks) && strings.HasPrefix(toks[i], "(") {
					action += " (" + strings.Join(parseColumnList(strings.Trim(toks[i], "()")), ", ") + ")"
					i++
				}
			default:
				return nil, 0, fmt.Errorf("unsupported referential action: %s", upperAt(i))
			}
			if event == "DELETE" {
				fk.OnDelete = action
			} else {
				fk.OnUpdate = action
			}
		case upperAt(i) == "DEFERRABLE":
			fk.Deferrable = true
			i++
		case upperAt(i) == "NOT" && upperAt(i+1) == "DEFERRABLE":
			fk.Deferrable = false
			i += 2
		case upperAt(i) == "NOT" && upperAt(i+1) == "VALID":
			fk.NotValid = true
			i += 2
		case upperAt(i) == "INITIALLY":
			fk.InitiallyDeferred = upperAt(i+1) == "DEFERRED"
			i += 2
		default:
			return fk, consumedOrig(i), nil
		}
	}
	return fk, consumedOrig(i), nil
}

// splitHeadParen splits "users(id)" into ("users", "(id)"). Tokens that start
// with "(" return ("", tok).
func splitHeadParen(tok string) (string, string) {
	idx := strings.Index(tok, "(")
	if idx <= 0 {
		if idx == 0 {
			return "", tok
		}
		return tok, ""
	}
	return tok[:idx], tok[idx:]
}

// parseForeignKeyDef parses "FOREIGN KEY (cols) REFERENCES ..." (as written in
// CREATE TABLE or returned by pg_get_constraintdef).
func parseForeignKeyDef(def string) (*ForeignKey, error) {
	tokens := tokenizeColumnDef(def)
	var toks []string
	for _, tok := range tokens {
		head, paren := splitHeadParen(tok)
		if head != "" {
			toks = append(toks, head)
		}
		if paren != "" {
			toks = append(toks, paren)
		}
	}
	if len(toks) < 4 || !strings.EqualFold(toks[0], "FOREIGN") || !strings.EqualFold(toks[1], "KEY") || !strings.HasPrefix(toks[2], "(") {
		return nil, fmt.Errorf("invalid foreign key: %s", def)
	}
	cols := parseColumnList(strings.TrimSuffix(strings.TrimPrefix(toks[2], "("), ")"))
	fk, _, err := parseReferences(toks, 3)
	if err != nil {
		return nil, fmt.Errorf("parsing foreign key %q: %w", def, err)
	}
	fk.Columns = cols
	return fk, nil
}

func extractCheckFromTokens(tokens []string, start int) string {
	var parts []string
	depth := 0
	for i := start; i < len(tokens); i++ {
		depth += parenDelta(tokens[i])
		parts = append(parts, tokens[i])
		if depth == 0 {
			break
		}
	}
	return strings.Join(parts, " ")
}

// --- Indexes ---

var indexHeadRe = regexp.MustCompile(`(?is)^CREATE\s+(UNIQUE\s+)?INDEX\s+(ASYNC\s+|CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(\w+)\s+ON\s+(?:ONLY\s+)?((?:\w+\.)?\w+)\s*(?:USING\s+\w+\s*)?\(`)

// parseIndexDef parses a CREATE INDEX statement. When requireAsync is true
// (desired-state files) the statement must use ASYNC; introspected
// definitions from pg_get_indexdef are parsed with requireAsync false.
func parseIndexDef(stmt string, requireAsync bool) (string, *Index, error) {
	stmt = strings.TrimSpace(stmt)
	m := indexHeadRe.FindStringSubmatchIndex(stmt)
	if m == nil {
		return "", nil, fmt.Errorf("unsupported statement: %s", truncate(stmt, 80))
	}
	isUnique := m[2] >= 0
	mode := ""
	if m[4] >= 0 {
		mode = strings.ToUpper(strings.TrimSpace(stmt[m[4]:m[5]]))
	}
	if requireAsync && mode != "ASYNC" {
		return "", nil, fmt.Errorf("CREATE INDEX must use ASYNC: %s", truncate(stmt, 80))
	}
	idx := &Index{
		Name:     strings.ToLower(stmt[m[6]:m[7]]),
		IsUnique: isUnique,
	}
	table := unqualify(stmt[m[8]:m[9]])

	keys, rest, err := takeParenGroup(stmt[m[1]-1:])
	if err != nil {
		return "", nil, fmt.Errorf("parsing index %s: %w", idx.Name, err)
	}
	for _, k := range splitAtTopLevel(keys, ',') {
		if k = normalizeIndexElemText(k); k != "" {
			idx.Columns = append(idx.Columns, k)
		}
	}

	for {
		rest = strings.TrimSpace(rest)
		upper := strings.ToUpper(rest)
		switch {
		case rest == "":
			return table, idx, nil
		case strings.HasPrefix(upper, "INCLUDE"):
			inner, r, err := takeParenGroup(strings.TrimSpace(rest[len("INCLUDE"):]))
			if err != nil {
				return "", nil, fmt.Errorf("parsing INCLUDE of index %s: %w", idx.Name, err)
			}
			idx.IncludeColumns = parseColumnList(inner)
			rest = r
		case nullsNotDistinctPrefixRe.MatchString(rest):
			idx.NullsNotDistinct = true
			rest = rest[len(nullsNotDistinctPrefixRe.FindString(rest)):]
		case nullsDistinctPrefixRe.MatchString(rest):
			rest = rest[len(nullsDistinctPrefixRe.FindString(rest)):]
		case strings.HasPrefix(upper, "WITH"):
			_, r, err := takeParenGroup(strings.TrimSpace(rest[len("WITH"):]))
			if err != nil {
				return "", nil, fmt.Errorf("parsing WITH of index %s: %w", idx.Name, err)
			}
			rest = r
		case strings.HasPrefix(upper, "WHERE"):
			idx.Where = strings.Join(strings.Fields(rest[len("WHERE"):]), " ")
			return table, idx, nil
		default:
			return "", nil, fmt.Errorf("unsupported index clause in %s: %s", idx.Name, truncate(rest, 60))
		}
	}
}

var (
	nullsNotDistinctPrefixRe = regexp.MustCompile(`(?i)^NULLS\s+NOT\s+DISTINCT`)
	nullsDistinctPrefixRe    = regexp.MustCompile(`(?i)^NULLS\s+DISTINCT`)
	simpleIndexElemRe        = regexp.MustCompile(`(?i)^\w+(\s+(ASC|DESC|NULLS\s+FIRST|NULLS\s+LAST))*$`)
)

// normalizeIndexElemText collapses whitespace and lowercases plain column
// references (expressions keep their original text).
func normalizeIndexElemText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if simpleIndexElemRe.MatchString(s) {
		return strings.ToLower(s)
	}
	return s
}

// takeParenGroup expects s to start with "(" and returns the contents of the
// balanced group and the remaining text after it.
func takeParenGroup(s string) (string, string, error) {
	if !strings.HasPrefix(s, "(") {
		return "", "", fmt.Errorf("expected '(' in: %s", truncate(s, 60))
	}
	depth := 0
	inQuote := false
	for i, ch := range s {
		switch {
		case ch == '\'':
			inQuote = !inQuote
		case inQuote:
		case ch == '(':
			depth++
		case ch == ')':
			depth--
			if depth == 0 {
				return s[1:i], s[i+1:], nil
			}
		}
	}
	return "", "", fmt.Errorf("unbalanced parentheses in: %s", truncate(s, 60))
}

// --- Statistics ---

var statsRe = regexp.MustCompile(`(?is)^CREATE\s+STATISTICS\s+(?:IF\s+NOT\s+EXISTS\s+)?((?:\w+\.)?\w+)\s*(?:\(\s*([A-Za-z_,\s]*)\))?\s*ON\s+(.+)\s+FROM\s+((?:\w+\.)?\w+)\s*$`)

// allStatisticsKinds is the default set when no kinds are listed.
var allStatisticsKinds = []string{"dependencies", "mcv", "ndistinct"}

// parseStatisticsDef parses CREATE STATISTICS (as written or as returned by
// pg_get_statisticsobjdef).
func parseStatisticsDef(stmt string) (string, *Statistics, error) {
	m := statsRe.FindStringSubmatch(strings.TrimSpace(stmt))
	if m == nil {
		return "", nil, fmt.Errorf("unsupported statement: %s", truncate(stmt, 80))
	}
	st := &Statistics{Name: unqualify(m[1])}
	for _, k := range strings.Split(m[2], ",") {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			st.Kinds = append(st.Kinds, k)
		}
	}
	for _, e := range splitAtTopLevel(m[3], ',') {
		if e = normalizeIndexElemText(e); e != "" {
			st.Expressions = append(st.Expressions, e)
		}
	}
	st.Kinds = normalizeStatisticsKinds(st.Kinds, len(st.Expressions))
	return unqualify(m[4]), st, nil
}

func normalizeStatisticsKinds(kinds []string, exprCount int) []string {
	if exprCount <= 1 {
		return nil // single-expression statistics have no kinds
	}
	if len(kinds) == 0 {
		return append([]string(nil), allStatisticsKinds...)
	}
	out := append([]string(nil), kinds...)
	sort.Strings(out)
	return out
}

// --- Sequences ---

var seqHeadRe = regexp.MustCompile(`(?is)^CREATE\s+SEQUENCE\s+(?:IF\s+NOT\s+EXISTS\s+)?((?:\w+\.)?\w+)(.*)$`)

func parseSequence(stmt string) (*Sequence, error) {
	m := seqHeadRe.FindStringSubmatch(strings.TrimSpace(stmt))
	if m == nil {
		return nil, fmt.Errorf("unsupported statement: %s", truncate(stmt, 80))
	}
	seq := &Sequence{Name: unqualify(m[1]), Increment: 1, Cache: 1}
	var minV, maxV, start *int64

	toks := strings.Fields(m[2])
	upperAt := func(j int) string {
		if j < len(toks) {
			return strings.ToUpper(toks[j])
		}
		return ""
	}
	num := func(j int) (int64, error) {
		if j >= len(toks) {
			return 0, fmt.Errorf("sequence %s: missing number", seq.Name)
		}
		v, err := strconv.ParseInt(toks[j], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("sequence %s: invalid number %q", seq.Name, toks[j])
		}
		return v, nil
	}

	for i := 0; i < len(toks); {
		var err error
		switch upperAt(i) {
		case "AS":
			i += 2
		case "INCREMENT":
			i++
			if upperAt(i) == "BY" {
				i++
			}
			seq.Increment, err = num(i)
			i++
		case "MINVALUE":
			v, e := num(i + 1)
			minV, err = &v, e
			i += 2
		case "MAXVALUE":
			v, e := num(i + 1)
			maxV, err = &v, e
			i += 2
		case "START":
			i++
			if upperAt(i) == "WITH" {
				i++
			}
			v, e := num(i)
			start, err = &v, e
			i++
		case "CACHE":
			seq.Cache, err = num(i + 1)
			i += 2
		case "CYCLE":
			seq.Cycle = true
			i++
		case "NO":
			switch upperAt(i + 1) {
			case "MINVALUE":
				minV = nil
			case "MAXVALUE":
				maxV = nil
			case "CYCLE":
				seq.Cycle = false
			}
			i += 2
		case "OWNED":
			// OWNED BY table.column | NONE
			if upperAt(i+2) != "NONE" && i+2 < len(toks) {
				seq.OwnedBy = strings.ToLower(unqualifyOwned(toks[i+2]))
			}
			i += 3
		default:
			return nil, fmt.Errorf("sequence %s: unsupported option %q", seq.Name, toks[i])
		}
		if err != nil {
			return nil, err
		}
	}

	applySequenceDefaults(seq, minV, maxV, start)
	return seq, nil
}

// applySequenceDefaults fills in PostgreSQL's defaults for unspecified bounds.
func applySequenceDefaults(seq *Sequence, minV, maxV, start *int64) {
	if seq.Increment > 0 {
		seq.MinValue, seq.MaxValue = 1, math.MaxInt64
	} else {
		seq.MinValue, seq.MaxValue = math.MinInt64, -1
	}
	if minV != nil {
		seq.MinValue = *minV
	}
	if maxV != nil {
		seq.MaxValue = *maxV
	}
	switch {
	case start != nil:
		seq.Start = *start
	case seq.Increment > 0:
		seq.Start = seq.MinValue
	default:
		seq.Start = seq.MaxValue
	}
}

// --- Generic helpers ---

// splitAtTopLevel splits a string by separator, but only at parenthesis depth 0.
func splitAtTopLevel(s string, sep rune) []string {
	var result []string
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

// extractParenColumns returns the columns of the first parenthesized group.
func extractParenColumns(s string) ([]string, error) {
	start := strings.Index(s, "(")
	if start < 0 {
		return nil, fmt.Errorf("no parenthesized column list in: %s", s)
	}
	inner, _, err := takeParenGroup(s[start:])
	if err != nil {
		return nil, fmt.Errorf("no parenthesized column list in: %s", s)
	}
	return parseColumnList(inner), nil
}

// extractParenExpr returns the first balanced parenthesized group, including
// the parentheses.
func extractParenExpr(s string) string {
	start := strings.Index(s, "(")
	if start < 0 {
		return s
	}
	inner, _, err := takeParenGroup(s[start:])
	if err != nil {
		return s
	}
	return "(" + inner + ")"
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

// unqualify lowercases a name and strips a "public." schema prefix.
func unqualify(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.TrimPrefix(name, "public.")
}

// unqualifyOwned strips a leading "public." from "public.table.column".
func unqualifyOwned(name string) string {
	if strings.Count(name, ".") == 2 {
		return name[strings.Index(name, ".")+1:]
	}
	return name
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
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
