package schema

import (
	"fmt"
	"math"
	"strings"
)

// generateCreateTable renders CREATE TABLE. Foreign keys listed in
// deferredFKs are omitted (they are added later with ALTER TABLE).
func generateCreateTable(t Table, deferredFKs map[string]bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s (\n", t.Name)

	var defs []string

	for _, col := range t.Columns {
		defs = append(defs, "    "+generateColumnDef(col))
	}

	if t.PrimaryKey != nil {
		defs = append(defs, fmt.Sprintf("    CONSTRAINT %s PRIMARY KEY (%s)",
			t.PrimaryKey.Name, strings.Join(t.PrimaryKey.Columns, ", ")))
	}

	for _, uc := range t.UniqueConstraints {
		defs = append(defs, "    "+generateUniqueConstraintDef(uc))
	}

	for _, cc := range t.CheckConstraints {
		defs = append(defs, fmt.Sprintf("    CONSTRAINT %s CHECK %s",
			cc.Name, cc.Expression))
	}

	for _, fk := range t.ForeignKeys {
		if deferredFKs[fk.Name] {
			continue
		}
		defs = append(defs, "    "+generateForeignKeyDef(fk))
	}

	b.WriteString(strings.Join(defs, ",\n"))
	b.WriteString("\n)")

	return b.String()
}

func generateColumnDef(col Column) string {
	var parts []string
	parts = append(parts, col.Name, strings.ToUpper(col.Type))

	if !col.IsNullable {
		parts = append(parts, "NOT NULL")
	}

	switch {
	case col.IsIdentity:
		parts = append(parts, generateIdentity(col))
	case col.Generated != "":
		parts = append(parts, "GENERATED ALWAYS AS", wrapParens(col.Generated), "STORED")
	case col.Default != "":
		parts = append(parts, "DEFAULT", col.Default)
	}

	return strings.Join(parts, " ")
}

func generateIdentity(col Column) string {
	s := fmt.Sprintf("GENERATED %s AS IDENTITY", col.IdentityGeneration)
	if col.IdentityOptions != "" {
		s += " (" + col.IdentityOptions + ")"
	}
	return s
}

func wrapParens(expr string) string {
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "(") {
		if _, rest, err := takeParenGroup(expr); err == nil && strings.TrimSpace(rest) == "" {
			return expr
		}
	}
	return "(" + expr + ")"
}

func generateUniqueConstraintDef(uc UniqueConstraint) string {
	nnd := ""
	if uc.NullsNotDistinct {
		nnd = "NULLS NOT DISTINCT "
	}
	return fmt.Sprintf("CONSTRAINT %s UNIQUE %s(%s)", uc.Name, nnd, strings.Join(uc.Columns, ", "))
}

func generateForeignKeyDef(fk ForeignKey) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
		fk.Name, strings.Join(fk.Columns, ", "), fk.RefTable, strings.Join(fk.RefColumns, ", "))
	if fk.MatchFull {
		b.WriteString(" MATCH FULL")
	}
	if fk.OnDelete != "" {
		b.WriteString(" ON DELETE " + fk.OnDelete)
	}
	if fk.OnUpdate != "" {
		b.WriteString(" ON UPDATE " + fk.OnUpdate)
	}
	if fk.Deferrable {
		b.WriteString(" DEFERRABLE")
		if fk.InitiallyDeferred {
			b.WriteString(" INITIALLY DEFERRED")
		}
	}
	return b.String()
}

func generateDropTable(name string) string {
	return fmt.Sprintf("DROP TABLE %s", name)
}

func generateAddColumn(table string, col Column) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, generateColumnDef(col))
}

func generateDropColumn(table, col string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", table, col)
}

func generateAlterColumn(table, col, action string) string {
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s", table, col, action)
}

func generateCreateIndex(table string, idx Index) string {
	unique := ""
	if idx.IsUnique {
		unique = "UNIQUE "
	}
	sql := fmt.Sprintf("CREATE %sINDEX ASYNC %s ON %s (%s)",
		unique, idx.Name, table, strings.Join(idx.Columns, ", "))
	if len(idx.IncludeColumns) > 0 {
		sql += fmt.Sprintf(" INCLUDE (%s)", strings.Join(idx.IncludeColumns, ", "))
	}
	if idx.NullsNotDistinct {
		sql += " NULLS NOT DISTINCT"
	}
	if idx.Where != "" {
		sql += " WHERE " + idx.Where
	}
	return sql
}

func generateDropIndex(name string) string {
	return fmt.Sprintf("DROP INDEX %s", name)
}

// generateAddCheckConstraint adds a CHECK constraint. Aurora DSQL requires
// NOT VALID; existing rows are checked later with VALIDATE CONSTRAINT.
func generateAddCheckConstraint(table string, cc CheckConstraint) string {
	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK %s NOT VALID",
		table, cc.Name, cc.Expression)
}

// generateAddForeignKey adds a FOREIGN KEY constraint with NOT VALID.
func generateAddForeignKey(table string, fk ForeignKey) string {
	return fmt.Sprintf("ALTER TABLE %s ADD %s NOT VALID", table, generateForeignKeyDef(fk))
}

// generateAddUniqueUsingIndex attaches a (fully built) unique index as a
// UNIQUE constraint. Aurora DSQL only supports ADD UNIQUE via USING INDEX.
func generateAddUniqueUsingIndex(table string, uc UniqueConstraint) string {
	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE USING INDEX %s",
		table, uc.Name, uc.Name)
}

func generateValidateConstraint(table, name string) string {
	return fmt.Sprintf("ALTER TABLE ASYNC %s VALIDATE CONSTRAINT %s", table, name)
}

func generateDropConstraint(table, name string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", table, name)
}

func generateCreateStatistics(table string, st Statistics) string {
	kinds := ""
	if len(st.Kinds) > 0 && strings.Join(st.Kinds, ",") != strings.Join(allStatisticsKinds, ",") {
		kinds = " (" + strings.Join(st.Kinds, ", ") + ")"
	}
	return fmt.Sprintf("CREATE STATISTICS %s%s ON %s FROM %s",
		st.Name, kinds, strings.Join(st.Expressions, ", "), table)
}

func generateDropStatistics(name string) string {
	return fmt.Sprintf("DROP STATISTICS %s", name)
}

func generateCreateSequence(seq Sequence) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE SEQUENCE %s CACHE %d", seq.Name, seq.Cache)
	if seq.Increment != 1 {
		fmt.Fprintf(&b, " INCREMENT BY %d", seq.Increment)
	}
	defMin, defMax := defaultSequenceBounds(seq.Increment)
	if seq.MinValue != defMin {
		fmt.Fprintf(&b, " MINVALUE %d", seq.MinValue)
	}
	if seq.MaxValue != defMax {
		fmt.Fprintf(&b, " MAXVALUE %d", seq.MaxValue)
	}
	if seq.Start != defaultSequenceStart(seq) {
		fmt.Fprintf(&b, " START WITH %d", seq.Start)
	}
	if seq.Cycle {
		b.WriteString(" CYCLE")
	}
	return b.String()
}

// generateAlterSequence renders ALTER SEQUENCE for the options that differ.
// It returns "" when nothing differs.
func generateAlterSequence(current, desired Sequence) string {
	var opts []string
	if current.Increment != desired.Increment {
		opts = append(opts, fmt.Sprintf("INCREMENT BY %d", desired.Increment))
	}
	if current.MinValue != desired.MinValue {
		opts = append(opts, fmt.Sprintf("MINVALUE %d", desired.MinValue))
	}
	if current.MaxValue != desired.MaxValue {
		opts = append(opts, fmt.Sprintf("MAXVALUE %d", desired.MaxValue))
	}
	if current.Start != desired.Start {
		opts = append(opts, fmt.Sprintf("START WITH %d", desired.Start))
	}
	if current.Cache != desired.Cache {
		opts = append(opts, fmt.Sprintf("CACHE %d", desired.Cache))
	}
	if current.Cycle != desired.Cycle {
		if desired.Cycle {
			opts = append(opts, "CYCLE")
		} else {
			opts = append(opts, "NO CYCLE")
		}
	}
	if len(opts) == 0 {
		return ""
	}
	return fmt.Sprintf("ALTER SEQUENCE %s %s", desired.Name, strings.Join(opts, " "))
}

func generateSequenceOwnedBy(seq Sequence) string {
	return fmt.Sprintf("ALTER SEQUENCE %s OWNED BY %s", seq.Name, seq.OwnedBy)
}

func generateDropSequence(name string) string {
	return fmt.Sprintf("DROP SEQUENCE IF EXISTS %s", name)
}

func defaultSequenceBounds(increment int64) (int64, int64) {
	if increment > 0 {
		return 1, math.MaxInt64
	}
	return math.MinInt64, -1
}

func defaultSequenceStart(seq Sequence) int64 {
	if seq.Increment > 0 {
		return seq.MinValue
	}
	return seq.MaxValue
}
