package migrate

import "tomodian/deesql/internal/schema"

// Action describes the type of change a statement performs.
type Action string

const (
	ActionCreate  Action = "+"   // A new resource will be provisioned.
	ActionUpdate  Action = "~"   // An existing resource will be modified in place.
	ActionDestroy Action = "-"   // An existing resource will be deleted.
	ActionReplace Action = "+/-" // An existing resource will be destroyed and recreated.
)

// HazardType is a string alias for hazard type identifiers.
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

// Statement is a single DDL statement in a migration plan.
type Statement struct {
	DDL      string
	Action   Action
	Resource string // e.g. "table.users", "index.idx_users_email"
	Hazards  []Hazard
}

// Plan is an ordered list of DDL statements to migrate a schema.
type Plan struct {
	Statements []Statement
}

// toInternal converts a public Plan to the internal schema.Plan.
func toInternal(p Plan) schema.Plan {
	stmts := make([]schema.Statement, len(p.Statements))
	for i, s := range p.Statements {
		hazards := make([]schema.Hazard, len(s.Hazards))
		for j, h := range s.Hazards {
			hazards[j] = schema.Hazard{Type: h.Type, Message: h.Message}
		}
		stmts[i] = schema.Statement{
			DDL:      s.DDL,
			Action:   schema.Action(s.Action),
			Resource: s.Resource,
			Hazards:  hazards,
		}
	}
	return schema.Plan{Statements: stmts}
}

// fromInternal converts an internal schema.Plan to the public Plan.
func fromInternal(p schema.Plan) Plan {
	stmts := make([]Statement, len(p.Statements))
	for i, s := range p.Statements {
		hazards := make([]Hazard, len(s.Hazards))
		for j, h := range s.Hazards {
			hazards[j] = Hazard{Type: h.Type, Message: h.Message}
		}
		stmts[i] = Statement{
			DDL:      s.DDL,
			Action:   Action(s.Action),
			Resource: s.Resource,
			Hazards:  hazards,
		}
	}
	return Plan{Statements: stmts}
}
