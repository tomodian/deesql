package schema

import "fmt"

// Action describes the type of change a statement performs.
type Action string

const (
	ActionCreate  Action = "+"   // A new resource will be provisioned.
	ActionUpdate  Action = "~"   // An existing resource will be modified in place.
	ActionDestroy Action = "-"   // An existing resource will be deleted.
	ActionReplace Action = "+/-" // An existing resource will be destroyed and recreated.
)

type HazardType = string

const (
	HazardDeletesData  HazardType = "DELETES_DATA"
	HazardIndexBuild   HazardType = "INDEX_BUILD"
	HazardIndexDropped HazardType = "INDEX_DROPPED"
	HazardCorrectness  HazardType = "CORRECTNESS"
)

// Hazard describes a potential risk of a migration statement.
type Hazard struct {
	Type    HazardType
	Message string
}

func (h Hazard) String() string {
	return fmt.Sprintf("%s: %s", h.Type, h.Message)
}

// Statement is a single DDL statement in a migration plan.
type Statement struct {
	DDL      string
	Action   Action
	Resource string // e.g. "table.users", "index.idx_users_email"
	Hazards  []Hazard
}

// ToSQL returns the DDL with a trailing semicolon.
func (s Statement) ToSQL() string {
	return s.DDL + ";"
}

// Plan is an ordered list of DDL statements to migrate a schema.
type Plan struct {
	Statements []Statement
}

// Summary returns counts of each action type.
func (p Plan) Summary() map[Action]int {
	counts := make(map[Action]int)
	for _, s := range p.Statements {
		counts[s.Action]++
	}
	return counts
}
