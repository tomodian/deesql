package schema

import (
	"context"
	"fmt"
	"strings"

	"tomodian/dsql-migrate/internal/ui"

	"github.com/go-playground/validator/v10"
)

var diffValidate = validator.New()

// DiffInput is the input for Diff.
type DiffInput struct {
	Current Schema
	Desired Schema
}

// DiffOutput is the output for Diff.
type DiffOutput struct {
	Plan Plan
}

// Diff compares the current (live) schema against the desired (parsed) schema
// and produces a migration Plan with ordered DDL statements.
func Diff(ctx context.Context, in DiffInput) (*DiffOutput, error) {
	ui.Step("Computing schema diff...")

	currentMap := tableMap(in.Current.Tables)
	desiredMap := tableMap(in.Desired.Tables)

	var stmts []Statement

	var (
		dropIndexes     []Statement
		dropConstraints []Statement
		dropTables      []Statement
		createTables    []Statement
		addColumns      []Statement
		addConstraints  []Statement
		createIndexes   []Statement
	)

	// Tables to drop.
	for name := range currentMap {
		if _, exists := desiredMap[name]; !exists {
			dropTables = append(dropTables, Statement{
				DDL:      generateDropTable(name),
				Action:   ActionDestroy,
				Resource: "table." + name,
				Hazards:  []Hazard{{Type: HazardDeletesData, Message: fmt.Sprintf("Drops table %s and all its data", name)}},
			})
		}
	}

	// Tables to create.
	for _, dt := range in.Desired.Tables {
		if _, exists := currentMap[dt.Name]; !exists {
			createTables = append(createTables, Statement{
				DDL:      generateCreateTable(dt),
				Action:   ActionCreate,
				Resource: "table." + dt.Name,
			})
			for _, idx := range dt.Indexes {
				createIndexes = append(createIndexes, Statement{
					DDL:      generateCreateIndex(dt.Name, idx),
					Action:   ActionCreate,
					Resource: "index." + idx.Name,
					Hazards:  []Hazard{{Type: HazardIndexBuild, Message: fmt.Sprintf("Building index %s asynchronously", idx.Name)}},
				})
			}
		}
	}

	// Tables to alter.
	for _, dt := range in.Desired.Tables {
		ct, exists := currentMap[dt.Name]
		if !exists {
			continue
		}

		alterStmts, err := diffTable(ct, dt)
		if err != nil {
			return nil, fmt.Errorf("diffing table %s: %w", dt.Name, err)
		}
		for _, s := range alterStmts {
			switch categorize(s.DDL) {
			case catDropIndex:
				dropIndexes = append(dropIndexes, s)
			case catDropConstraint:
				dropConstraints = append(dropConstraints, s)
			case catAddColumn:
				addColumns = append(addColumns, s)
			case catAddConstraint:
				addConstraints = append(addConstraints, s)
			case catCreateIndex:
				createIndexes = append(createIndexes, s)
			}
		}
	}

	// Assemble in order.
	stmts = append(stmts, dropIndexes...)
	stmts = append(stmts, dropConstraints...)
	stmts = append(stmts, dropTables...)
	stmts = append(stmts, createTables...)
	stmts = append(stmts, addColumns...)
	stmts = append(stmts, addConstraints...)
	stmts = append(stmts, createIndexes...)

	ui.Dim("    %d statement(s)\n", len(stmts))
	return &DiffOutput{Plan: Plan{Statements: stmts}}, nil
}

func diffTable(current, desired Table) ([]Statement, error) {
	var stmts []Statement

	// --- Columns ---
	currentCols := columnMap(current.Columns)
	desiredCols := columnMap(desired.Columns)

	for _, cc := range current.Columns {
		if _, exists := desiredCols[cc.Name]; !exists {
			return nil, fmt.Errorf(
				"DROP COLUMN not supported by Aurora DSQL: cannot drop column %s.%s",
				current.Name, cc.Name)
		}
	}

	for _, dc := range desired.Columns {
		cc, exists := currentCols[dc.Name]
		if !exists {
			stmts = append(stmts, Statement{
				DDL:      generateAddColumn(desired.Name, dc),
				Action:   ActionUpdate,
				Resource: "table." + desired.Name,
			})
			continue
		}

		if NormalizeType(cc.Type) != NormalizeType(dc.Type) {
			return nil, fmt.Errorf(
				"ALTER COLUMN TYPE not supported by Aurora DSQL: cannot change %s.%s from %s to %s",
				current.Name, dc.Name, cc.Type, dc.Type)
		}

		if cc.IsNullable != dc.IsNullable {
			return nil, fmt.Errorf(
				"ALTER COLUMN SET/DROP NOT NULL not supported by Aurora DSQL: column %s.%s",
				current.Name, dc.Name)
		}

		if normalizeDefault(cc.Default) != normalizeDefault(dc.Default) {
			return nil, fmt.Errorf(
				"ALTER COLUMN SET/DROP DEFAULT not supported by Aurora DSQL: column %s.%s",
				current.Name, dc.Name)
		}
	}

	// --- Primary Key ---
	if err := diffPrimaryKey(current, desired); err != nil {
		return nil, err
	}

	// --- Unique Constraints ---
	currentUCs := constraintMap(current.UniqueConstraints)
	desiredUCs := constraintMap(desired.UniqueConstraints)

	for _, cuc := range current.UniqueConstraints {
		key := constraintKey(cuc.Columns)
		if _, exists := desiredUCs[key]; !exists {
			stmts = append(stmts, Statement{
				DDL:      generateDropConstraint(current.Name, cuc.Name),
				Action:   ActionDestroy,
				Resource: "constraint." + current.Name + "." + cuc.Name,
			})
		}
	}

	for _, duc := range desired.UniqueConstraints {
		key := constraintKey(duc.Columns)
		if _, exists := currentUCs[key]; !exists {
			stmts = append(stmts, Statement{
				DDL:      generateAddUniqueConstraint(desired.Name, duc),
				Action:   ActionCreate,
				Resource: "constraint." + desired.Name + "." + duc.Name,
			})
		}
	}

	// --- Check Constraints ---
	currentCCs := checkMap(current.CheckConstraints)
	desiredCCs := checkMap(desired.CheckConstraints)

	for _, ccc := range current.CheckConstraints {
		if _, exists := desiredCCs[normalizeCheck(ccc.Expression)]; !exists {
			stmts = append(stmts, Statement{
				DDL:      generateDropConstraint(current.Name, ccc.Name),
				Action:   ActionDestroy,
				Resource: "constraint." + current.Name + "." + ccc.Name,
			})
		}
	}

	for _, dcc := range desired.CheckConstraints {
		if _, exists := currentCCs[normalizeCheck(dcc.Expression)]; !exists {
			stmts = append(stmts, Statement{
				DDL:      generateAddCheckConstraint(desired.Name, dcc),
				Action:   ActionCreate,
				Resource: "constraint." + desired.Name + "." + dcc.Name,
			})
		}
	}

	// --- Indexes ---
	currentIdxs := indexMap(current.Indexes)
	desiredIdxs := indexMap(desired.Indexes)

	for _, ci := range current.Indexes {
		di, exists := desiredIdxs[ci.Name]
		if !exists {
			stmts = append(stmts, Statement{
				DDL:      generateDropIndex(ci.Name),
				Action:   ActionDestroy,
				Resource: "index." + ci.Name,
				Hazards:  []Hazard{{Type: HazardIndexDropped, Message: fmt.Sprintf("Dropping index %s may degrade query performance", ci.Name)}},
			})
			continue
		}
		if indexKey(ci) != indexKey(di) {
			stmts = append(stmts, Statement{
				DDL:      generateDropIndex(ci.Name),
				Action:   ActionReplace,
				Resource: "index." + ci.Name,
				Hazards:  []Hazard{{Type: HazardIndexDropped, Message: fmt.Sprintf("Dropping index %s for recreation", ci.Name)}},
			})
			stmts = append(stmts, Statement{
				DDL:      generateCreateIndex(desired.Name, di),
				Action:   ActionReplace,
				Resource: "index." + di.Name,
				Hazards:  []Hazard{{Type: HazardIndexBuild, Message: fmt.Sprintf("Rebuilding index %s asynchronously", di.Name)}},
			})
		}
	}

	for _, di := range desired.Indexes {
		if _, exists := currentIdxs[di.Name]; !exists {
			stmts = append(stmts, Statement{
				DDL:      generateCreateIndex(desired.Name, di),
				Action:   ActionCreate,
				Resource: "index." + di.Name,
				Hazards:  []Hazard{{Type: HazardIndexBuild, Message: fmt.Sprintf("Building index %s asynchronously", di.Name)}},
			})
		}
	}

	return stmts, nil
}

func diffPrimaryKey(current, desired Table) error {
	cPK := current.PrimaryKey
	dPK := desired.PrimaryKey

	if cPK == nil && dPK == nil {
		return nil
	}
	if cPK != nil && dPK != nil && constraintKey(cPK.Columns) == constraintKey(dPK.Columns) {
		return nil
	}

	return fmt.Errorf(
		"PRIMARY KEY change not supported by Aurora DSQL: table %s requires recreation",
		current.Name)
}

// --- Helpers ---

func tableMap(tables []Table) map[string]Table {
	m := make(map[string]Table, len(tables))
	for _, t := range tables {
		m[t.Name] = t
	}
	return m
}

func columnMap(cols []Column) map[string]Column {
	m := make(map[string]Column, len(cols))
	for _, c := range cols {
		m[c.Name] = c
	}
	return m
}

func constraintMap(ucs []UniqueConstraint) map[string]UniqueConstraint {
	m := make(map[string]UniqueConstraint, len(ucs))
	for _, uc := range ucs {
		m[constraintKey(uc.Columns)] = uc
	}
	return m
}

func checkMap(ccs []CheckConstraint) map[string]CheckConstraint {
	m := make(map[string]CheckConstraint, len(ccs))
	for _, cc := range ccs {
		m[normalizeCheck(cc.Expression)] = cc
	}
	return m
}

func indexMap(idxs []Index) map[string]Index {
	m := make(map[string]Index, len(idxs))
	for _, idx := range idxs {
		m[idx.Name] = idx
	}
	return m
}

func constraintKey(cols []string) string {
	return fmt.Sprintf("%v", cols)
}

func indexKey(idx Index) string {
	return fmt.Sprintf("%v:%v", idx.Columns, idx.IsUnique)
}

type stmtCategory int

const (
	catDropIndex stmtCategory = iota
	catDropConstraint
	catAddColumn
	catAddConstraint
	catCreateIndex
)

func categorize(ddl string) stmtCategory {
	upper := strings.ToUpper(ddl)
	switch {
	case strings.HasPrefix(upper, "DROP INDEX"):
		return catDropIndex
	case strings.Contains(upper, "DROP CONSTRAINT"):
		return catDropConstraint
	case strings.Contains(upper, "ADD COLUMN"):
		return catAddColumn
	case strings.HasPrefix(upper, "CREATE") && strings.Contains(upper, "INDEX"):
		return catCreateIndex
	default:
		return catAddConstraint
	}
}
