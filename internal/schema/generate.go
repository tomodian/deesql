package schema

import (
	"fmt"
	"strings"
)

func generateCreateTable(t Table) string {
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
		defs = append(defs, fmt.Sprintf("    CONSTRAINT %s UNIQUE (%s)",
			uc.Name, strings.Join(uc.Columns, ", ")))
	}

	for _, cc := range t.CheckConstraints {
		defs = append(defs, fmt.Sprintf("    CONSTRAINT %s CHECK %s",
			cc.Name, cc.Expression))
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

	if col.IsIdentity {
		parts = append(parts, "GENERATED", col.IdentityGeneration, "AS IDENTITY")
	} else if col.Default != "" {
		parts = append(parts, "DEFAULT", col.Default)
	}

	return strings.Join(parts, " ")
}

func generateDropTable(name string) string {
	return fmt.Sprintf("DROP TABLE %s", name)
}

func generateAddColumn(table string, col Column) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, generateColumnDef(col))
}

func generateCreateIndex(table string, idx Index) string {
	unique := ""
	if idx.IsUnique {
		unique = "UNIQUE "
	}
	return fmt.Sprintf("CREATE %sINDEX ASYNC %s ON %s (%s)",
		unique, idx.Name, table, strings.Join(idx.Columns, ", "))
}

func generateDropIndex(name string) string {
	return fmt.Sprintf("DROP INDEX %s", name)
}

func generateAddCheckConstraint(table string, cc CheckConstraint) string {
	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK %s",
		table, cc.Name, cc.Expression)
}

func generateAddUniqueConstraint(table string, uc UniqueConstraint) string {
	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s)",
		table, uc.Name, strings.Join(uc.Columns, ", "))
}

func generateDropConstraint(table, name string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", table, name)
}
