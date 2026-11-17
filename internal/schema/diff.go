package schema

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/tomodian/deesql/internal/ui"
)

// DiffInput is the input for Diff.
type DiffInput struct {
	Current Schema
	Desired Schema
}

// DiffOutput is the output for Diff.
type DiffOutput struct {
	Plan Plan
}

// phase orders statements in the plan. Each DDL runs in its own transaction,
// so dependencies are satisfied purely by ordering.
type phase int

const (
	phaseDropIndex phase = iota
	phaseDropConstraint
	phaseDropStatistics
	phaseDropColumn
	phaseDropTable
	phaseCreateSequence
	phaseAlterSequence
	phaseCreateTable
	phaseAlterColumn
	phaseCreateIndex
	phaseAddConstraint
	phaseValidateConstraint
	phaseCreateStatistics
	phaseDropSequence
	numPhases
)

type planBuilder struct {
	buckets [numPhases][]Statement
}

func (b *planBuilder) add(p phase, s Statement) {
	b.buckets[p] = append(b.buckets[p], s)
}

func (b *planBuilder) statements() []Statement {
	var out []Statement
	for _, bucket := range b.buckets {
		out = append(out, bucket...)
	}
	return out
}

// Diff compares the current (live) schema against the desired (parsed) schema
// and produces a migration Plan with ordered DDL statements.
func Diff(ctx context.Context, in DiffInput) (*DiffOutput, error) {
	ui.Step("Computing schema diff...")

	currentMap := tableMap(in.Current.Tables)
	desiredMap := tableMap(in.Desired.Tables)
	b := &planBuilder{}

	diffSequences(b, in.Current.Sequences, in.Desired.Sequences)

	// Tables to drop: referencing tables first. FKs forming cycles between
	// dropped tables are dropped up front.
	var dropped []Table
	for _, ct := range in.Current.Tables {
		if _, exists := desiredMap[ct.Name]; !exists {
			dropped = append(dropped, ct)
		}
	}
	droppedOrder, droppedCycles := orderByForeignKeys(dropped)
	for _, ct := range droppedOrder {
		for _, fk := range ct.ForeignKeys {
			if droppedCycles[ct.Name][fk.Name] {
				b.add(phaseDropConstraint, Statement{
					DDL:      generateDropConstraint(ct.Name, fk.Name),
					Action:   ActionDestroy,
					Resource: "constraint." + ct.Name + "." + fk.Name,
				})
			}
		}
	}
	for i := len(droppedOrder) - 1; i >= 0; i-- {
		name := droppedOrder[i].Name
		b.add(phaseDropTable, Statement{
			DDL:      generateDropTable(name),
			Action:   ActionDestroy,
			Resource: "table." + name,
			Hazards:  []Hazard{{Type: HazardDeletesData, Message: fmt.Sprintf("Drops table %s and all its data", name)}},
		})
	}

	// Tables to create: referenced tables first. FKs that point at a table
	// created later (cycles) are added with ALTER TABLE afterwards.
	var created []Table
	for _, dt := range in.Desired.Tables {
		if _, exists := currentMap[dt.Name]; !exists {
			created = append(created, dt)
		}
	}
	createdOrder, createdDeferred := orderByForeignKeys(created)
	for _, dt := range createdOrder {
		deferred := createdDeferred[dt.Name]
		b.add(phaseCreateTable, Statement{
			DDL:      generateCreateTable(dt, deferred),
			Action:   ActionCreate,
			Resource: "table." + dt.Name,
		})
		for _, fk := range dt.ForeignKeys {
			if deferred[fk.Name] {
				addForeignKey(b, dt.Name, fk, false)
			}
		}
		for _, idx := range dt.Indexes {
			b.add(phaseCreateIndex, createIndexStatement(dt.Name, idx, ActionCreate))
		}
		for _, st := range dt.Statistics {
			b.add(phaseCreateStatistics, Statement{
				DDL:      generateCreateStatistics(dt.Name, st),
				Action:   ActionCreate,
				Resource: "statistics." + st.Name,
			})
		}
	}

	// Tables to alter.
	for _, dt := range in.Desired.Tables {
		ct, exists := currentMap[dt.Name]
		if !exists {
			continue
		}
		if err := diffTable(b, ct, dt); err != nil {
			return nil, fmt.Errorf("diffing table %s: %w", dt.Name, err)
		}
	}

	stmts := b.statements()
	ui.Dim("    %d statement(s)\n", len(stmts))
	return &DiffOutput{Plan: Plan{Statements: stmts}}, nil
}

var identityCacheRe = regexp.MustCompile(`(?i)\bCACHE\s+\d+`)

func diffTable(b *planBuilder, current, desired Table) error {
	if err := diffColumns(b, current, desired); err != nil {
		return err
	}

	if err := diffPrimaryKey(current, desired); err != nil {
		return err
	}

	diffUniqueConstraints(b, current, desired)
	diffCheckConstraints(b, current, desired)
	diffForeignKeys(b, current, desired)
	diffIndexes(b, current, desired)
	diffStatistics(b, current, desired)
	return nil
}

func diffColumns(b *planBuilder, current, desired Table) error {
	currentCols := columnMap(current.Columns)
	desiredCols := columnMap(desired.Columns)

	pkCols := map[string]bool{}
	if current.PrimaryKey != nil {
		for _, c := range current.PrimaryKey.Columns {
			pkCols[c] = true
		}
	}

	for _, cc := range current.Columns {
		if _, exists := desiredCols[cc.Name]; exists {
			continue
		}
		if pkCols[cc.Name] {
			return fmt.Errorf(
				"DROP COLUMN of a primary key column is not supported by Aurora DSQL: %s.%s (recreate the table instead)",
				current.Name, cc.Name)
		}
		b.add(phaseDropColumn, Statement{
			DDL:      generateDropColumn(current.Name, cc.Name),
			Action:   ActionDestroy,
			Resource: "column." + current.Name + "." + cc.Name,
			Hazards:  []Hazard{{Type: HazardDeletesData, Message: fmt.Sprintf("Drops column %s.%s and its data", current.Name, cc.Name)}},
		})
	}

	for _, dc := range desired.Columns {
		cc, exists := currentCols[dc.Name]
		if !exists {
			b.add(phaseAlterColumn, Statement{
				DDL:      generateAddColumn(desired.Name, dc),
				Action:   ActionUpdate,
				Resource: "table." + desired.Name,
			})
			continue
		}
		if err := diffColumn(b, desired.Name, cc, dc); err != nil {
			return err
		}
	}
	return nil
}

func diffColumn(b *planBuilder, table string, cc, dc Column) error {
	resource := "column." + table + "." + dc.Name
	alter := func(action string, hazards ...Hazard) {
		b.add(phaseAlterColumn, Statement{
			DDL:      generateAlterColumn(table, dc.Name, action),
			Action:   ActionUpdate,
			Resource: resource,
			Hazards:  hazards,
		})
	}

	if !typesEqual(cc.Type, dc.Type) {
		return fmt.Errorf(
			"ALTER COLUMN TYPE not supported by Aurora DSQL: cannot change %s.%s from %s to %s",
			table, dc.Name, cc.Type, dc.Type)
	}

	// Stored generated columns: only DROP EXPRESSION is supported.
	switch {
	case cc.Generated != "" && dc.Generated == "":
		alter("DROP EXPRESSION")
	case cc.Generated == "" && dc.Generated != "":
		return fmt.Errorf(
			"adding a generation expression is not supported by Aurora DSQL: column %s.%s (recreate the column instead)",
			table, dc.Name)
	case cc.Generated != "" && normalizeExpr(cc.Generated) != normalizeExpr(dc.Generated):
		return fmt.Errorf(
			"changing a generation expression is not supported by Aurora DSQL: column %s.%s (recreate the column instead)",
			table, dc.Name)
	}

	// Identity.
	switch {
	case !cc.IsIdentity && dc.IsIdentity:
		if NormalizeType(dc.Type) != "bigint" {
			return fmt.Errorf("identity columns must be bigint on Aurora DSQL: column %s.%s is %s", table, dc.Name, dc.Type)
		}
		if !identityCacheRe.MatchString(dc.IdentityOptions) {
			return fmt.Errorf("ADD GENERATED AS IDENTITY requires an explicit CACHE on Aurora DSQL: column %s.%s", table, dc.Name)
		}
		if cc.Default != "" {
			alter("DROP DEFAULT")
		}
		alter("ADD " + generateIdentity(dc))
	case cc.IsIdentity && !dc.IsIdentity:
		alter("DROP IDENTITY", Hazard{Type: HazardCorrectness, Message: fmt.Sprintf("Column %s.%s will no longer generate values", table, dc.Name)})
	case cc.IsIdentity && dc.IsIdentity && cc.IdentityGeneration != dc.IdentityGeneration:
		alter("SET GENERATED " + dc.IdentityGeneration)
	}

	// Nullability: only DROP NOT NULL is supported.
	switch {
	case !cc.IsNullable && dc.IsNullable:
		alter("DROP NOT NULL")
	case cc.IsNullable && !dc.IsNullable:
		return fmt.Errorf(
			"ALTER COLUMN SET NOT NULL not supported by Aurora DSQL: column %s.%s",
			table, dc.Name)
	}

	// Defaults (identity and generated columns have none).
	if !dc.IsIdentity && dc.Generated == "" && !cc.IsIdentity && cc.Generated == "" &&
		normalizeDefault(cc.Default) != normalizeDefault(dc.Default) {
		if dc.Default == "" {
			alter("DROP DEFAULT")
		} else {
			alter("SET DEFAULT " + dc.Default)
		}
	}
	return nil
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

func diffUniqueConstraints(b *planBuilder, current, desired Table) {
	currentUCs := uniqueMap(current.UniqueConstraints)
	desiredUCs := uniqueMap(desired.UniqueConstraints)

	for _, cuc := range current.UniqueConstraints {
		if _, exists := desiredUCs[uniqueKey(cuc)]; !exists {
			b.add(phaseDropConstraint, dropConstraintStatement(current.Name, cuc.Name))
		}
	}

	// ADD UNIQUE is only supported via a fully built unique index.
	for _, duc := range desired.UniqueConstraints {
		if _, exists := currentUCs[uniqueKey(duc)]; exists {
			continue
		}
		idx := createIndexStatement(desired.Name, Index{
			Name:             duc.Name,
			Columns:          duc.Columns,
			IsUnique:         true,
			NullsNotDistinct: duc.NullsNotDistinct,
		}, ActionCreate)
		idx.Resource = "constraint." + desired.Name + "." + duc.Name
		idx.WaitForJob = true
		b.add(phaseCreateIndex, idx)
		b.add(phaseAddConstraint, Statement{
			DDL:      generateAddUniqueUsingIndex(desired.Name, duc),
			Action:   ActionCreate,
			Resource: "constraint." + desired.Name + "." + duc.Name,
		})
	}
}

func diffCheckConstraints(b *planBuilder, current, desired Table) {
	currentCCs := checkMap(current.CheckConstraints)
	desiredCCs := checkMap(desired.CheckConstraints)

	for _, ccc := range current.CheckConstraints {
		if _, exists := desiredCCs[normalizeCheck(ccc.Expression)]; !exists {
			b.add(phaseDropConstraint, dropConstraintStatement(current.Name, ccc.Name))
		}
	}

	for _, dcc := range desired.CheckConstraints {
		ccc, exists := currentCCs[normalizeCheck(dcc.Expression)]
		switch {
		case !exists:
			b.add(phaseAddConstraint, Statement{
				DDL:      generateAddCheckConstraint(desired.Name, dcc),
				Action:   ActionCreate,
				Resource: "constraint." + desired.Name + "." + dcc.Name,
			})
			b.add(phaseValidateConstraint, validateStatement(desired.Name, dcc.Name, true))
		case ccc.NotValid:
			b.add(phaseValidateConstraint, validateStatement(desired.Name, ccc.Name, true))
		}
	}
}

func diffForeignKeys(b *planBuilder, current, desired Table) {
	currentFKs := foreignKeyMap(current.ForeignKeys)
	desiredFKs := foreignKeyMap(desired.ForeignKeys)

	for _, cfk := range current.ForeignKeys {
		if _, exists := desiredFKs[foreignKeyKey(cfk)]; !exists {
			b.add(phaseDropConstraint, dropConstraintStatement(current.Name, cfk.Name))
		}
	}

	for _, dfk := range desired.ForeignKeys {
		cfk, exists := currentFKs[foreignKeyKey(dfk)]
		switch {
		case !exists:
			addForeignKey(b, desired.Name, dfk, true)
		case cfk.NotValid:
			b.add(phaseValidateConstraint, validateStatement(desired.Name, cfk.Name, true))
		}
	}
}

func diffIndexes(b *planBuilder, current, desired Table) {
	currentIdxs := indexMap(current.Indexes)
	desiredIdxs := indexMap(desired.Indexes)

	for _, ci := range current.Indexes {
		di, exists := desiredIdxs[ci.Name]
		if !exists {
			b.add(phaseDropIndex, Statement{
				DDL:      generateDropIndex(ci.Name),
				Action:   ActionDestroy,
				Resource: "index." + ci.Name,
				Hazards:  []Hazard{{Type: HazardIndexDropped, Message: fmt.Sprintf("Dropping index %s may degrade query performance", ci.Name)}},
			})
			continue
		}
		if indexKey(ci) != indexKey(di) {
			b.add(phaseDropIndex, Statement{
				DDL:      generateDropIndex(ci.Name),
				Action:   ActionReplace,
				Resource: "index." + ci.Name,
				Hazards:  []Hazard{{Type: HazardIndexDropped, Message: fmt.Sprintf("Dropping index %s for recreation", ci.Name)}},
			})
			s := createIndexStatement(desired.Name, di, ActionReplace)
			s.Hazards = []Hazard{{Type: HazardIndexBuild, Message: fmt.Sprintf("Rebuilding index %s asynchronously", di.Name)}}
			b.add(phaseCreateIndex, s)
		}
	}

	for _, di := range desired.Indexes {
		if _, exists := currentIdxs[di.Name]; !exists {
			b.add(phaseCreateIndex, createIndexStatement(desired.Name, di, ActionCreate))
		}
	}
}

func diffStatistics(b *planBuilder, current, desired Table) {
	currentStats := statisticsMap(current.Statistics)
	desiredStats := statisticsMap(desired.Statistics)

	for _, cs := range current.Statistics {
		ds, exists := desiredStats[cs.Name]
		switch {
		case !exists:
			b.add(phaseDropStatistics, Statement{
				DDL:      generateDropStatistics(cs.Name),
				Action:   ActionDestroy,
				Resource: "statistics." + cs.Name,
			})
		case statisticsKey(cs) != statisticsKey(ds):
			b.add(phaseDropStatistics, Statement{
				DDL:      generateDropStatistics(cs.Name),
				Action:   ActionReplace,
				Resource: "statistics." + cs.Name,
			})
			b.add(phaseCreateStatistics, Statement{
				DDL:      generateCreateStatistics(desired.Name, ds),
				Action:   ActionReplace,
				Resource: "statistics." + ds.Name,
			})
		}
	}

	for _, ds := range desired.Statistics {
		if _, exists := currentStats[ds.Name]; !exists {
			b.add(phaseCreateStatistics, Statement{
				DDL:      generateCreateStatistics(desired.Name, ds),
				Action:   ActionCreate,
				Resource: "statistics." + ds.Name,
			})
		}
	}
}

func diffSequences(b *planBuilder, current, desired []Sequence) {
	currentSeqs := make(map[string]Sequence, len(current))
	for _, s := range current {
		currentSeqs[s.Name] = s
	}
	desiredSeqs := make(map[string]Sequence, len(desired))
	for _, s := range desired {
		desiredSeqs[s.Name] = s
	}

	for _, ds := range desired {
		cs, exists := currentSeqs[ds.Name]
		if !exists {
			b.add(phaseCreateSequence, Statement{
				DDL:      generateCreateSequence(ds),
				Action:   ActionCreate,
				Resource: "sequence." + ds.Name,
			})
			if ds.OwnedBy != "" {
				b.add(phaseAlterColumn, Statement{
					DDL:      generateSequenceOwnedBy(ds),
					Action:   ActionUpdate,
					Resource: "sequence." + ds.Name,
				})
			}
			continue
		}
		if ddl := generateAlterSequence(cs, ds); ddl != "" {
			b.add(phaseAlterSequence, Statement{
				DDL:      ddl,
				Action:   ActionUpdate,
				Resource: "sequence." + ds.Name,
			})
		}
	}

	for _, cs := range current {
		if _, exists := desiredSeqs[cs.Name]; !exists {
			b.add(phaseDropSequence, Statement{
				DDL:      generateDropSequence(cs.Name),
				Action:   ActionDestroy,
				Resource: "sequence." + cs.Name,
				Hazards:  []Hazard{{Type: HazardDeletesData, Message: fmt.Sprintf("Drops sequence %s and its current value", cs.Name)}},
			})
		}
	}
}

// --- Statement helpers ---

func createIndexStatement(table string, idx Index, action Action) Statement {
	return Statement{
		DDL:      generateCreateIndex(table, idx),
		Action:   action,
		Resource: "index." + idx.Name,
		Hazards:  []Hazard{{Type: HazardIndexBuild, Message: fmt.Sprintf("Building index %s asynchronously", idx.Name)}},
	}
}

func dropConstraintStatement(table, name string) Statement {
	return Statement{
		DDL:      generateDropConstraint(table, name),
		Action:   ActionDestroy,
		Resource: "constraint." + table + "." + name,
		Hazards:  []Hazard{{Type: HazardCorrectness, Message: fmt.Sprintf("Dropping constraint %s removes data-integrity enforcement on %s", name, table)}},
	}
}

// addForeignKey adds a FK with NOT VALID and validates it asynchronously.
// existingTable controls whether validation is flagged as a hazard (a table
// created in the same plan has no rows to violate the constraint).
func addForeignKey(b *planBuilder, table string, fk ForeignKey, existingTable bool) {
	b.add(phaseAddConstraint, Statement{
		DDL:      generateAddForeignKey(table, fk),
		Action:   ActionCreate,
		Resource: "constraint." + table + "." + fk.Name,
	})
	b.add(phaseValidateConstraint, validateStatement(table, fk.Name, existingTable))
}

func validateStatement(table, name string, hazardous bool) Statement {
	s := Statement{
		DDL:      generateValidateConstraint(table, name),
		Action:   ActionUpdate,
		Resource: "constraint." + table + "." + name,
	}
	if hazardous {
		s.Hazards = []Hazard{{
			Type:    HazardConstraintValidation,
			Message: fmt.Sprintf("Validating %s asynchronously; the job fails if existing rows in %s violate it", name, table),
		}}
	}
	return s
}

// orderByForeignKeys sorts tables so that referenced tables come before the
// tables referencing them (considering only references within the given
// set). FKs that close a cycle are returned per table as "deferred".
func orderByForeignKeys(tables []Table) ([]Table, map[string]map[string]bool) {
	byName := tableMap(tables)
	state := map[string]int{} // 0 = unvisited, 1 = visiting, 2 = done
	deferred := map[string]map[string]bool{}
	var ordered []Table

	var visit func(t Table)
	visit = func(t Table) {
		state[t.Name] = 1
		for _, fk := range t.ForeignKeys {
			ref, ok := byName[fk.RefTable]
			if !ok || fk.RefTable == t.Name {
				continue
			}
			switch state[ref.Name] {
			case 0:
				visit(ref)
			case 1:
				if deferred[t.Name] == nil {
					deferred[t.Name] = map[string]bool{}
				}
				deferred[t.Name][fk.Name] = true
			}
		}
		state[t.Name] = 2
		ordered = append(ordered, t)
	}

	for _, t := range tables {
		if state[t.Name] == 0 {
			visit(t)
		}
	}
	return ordered, deferred
}

// --- Map and key helpers ---

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

func uniqueMap(ucs []UniqueConstraint) map[string]UniqueConstraint {
	m := make(map[string]UniqueConstraint, len(ucs))
	for _, uc := range ucs {
		m[uniqueKey(uc)] = uc
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

func foreignKeyMap(fks []ForeignKey) map[string]ForeignKey {
	m := make(map[string]ForeignKey, len(fks))
	for _, fk := range fks {
		m[foreignKeyKey(fk)] = fk
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

func statisticsMap(stats []Statistics) map[string]Statistics {
	m := make(map[string]Statistics, len(stats))
	for _, st := range stats {
		m[st.Name] = st
	}
	return m
}

func constraintKey(cols []string) string {
	return fmt.Sprintf("%v", cols)
}

func uniqueKey(uc UniqueConstraint) string {
	return fmt.Sprintf("%v:%v", uc.Columns, uc.NullsNotDistinct)
}

func foreignKeyKey(fk ForeignKey) string {
	return fmt.Sprintf("%v->%s%v:del=%s:upd=%s:full=%v:def=%v:init=%v",
		fk.Columns, fk.RefTable, fk.RefColumns,
		strings.ToUpper(fk.OnDelete), strings.ToUpper(fk.OnUpdate),
		fk.MatchFull, fk.Deferrable, fk.Deferrable && fk.InitiallyDeferred)
}

func indexKey(idx Index) string {
	cols := make([]string, len(idx.Columns))
	for i, c := range idx.Columns {
		cols[i] = normalizeIndexElem(c)
	}
	return fmt.Sprintf("%v:%v:%v:%v:%s", cols, idx.IncludeColumns, idx.IsUnique, idx.NullsNotDistinct, normalizeExpr(idx.Where))
}

func statisticsKey(st Statistics) string {
	exprs := make([]string, len(st.Expressions))
	for i, e := range st.Expressions {
		exprs[i] = normalizeExpr(e)
	}
	return fmt.Sprintf("%v:%v", normalizeStatisticsKinds(st.Kinds, len(st.Expressions)), exprs)
}
