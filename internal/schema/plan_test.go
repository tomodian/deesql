package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHazard(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		h := Hazard{Type: HazardDeletesData, Message: "drops all data"}
		assert.Equal(t, "DELETES_DATA: drops all data", h.String())
	})

	t.Run("String with index build", func(t *testing.T) {
		h := Hazard{Type: HazardIndexBuild, Message: "building index"}
		assert.Equal(t, "INDEX_BUILD: building index", h.String())
	})
}

func TestStatement(t *testing.T) {
	t.Run("ToSQL appends semicolon", func(t *testing.T) {
		s := Statement{DDL: "CREATE TABLE t (id TEXT)"}
		assert.Equal(t, "CREATE TABLE t (id TEXT);", s.ToSQL())
	})

	t.Run("ToSQL with existing content", func(t *testing.T) {
		s := Statement{
			DDL:     "DROP TABLE users",
			Hazards: []Hazard{{Type: HazardDeletesData, Message: "drops data"}},
		}
		assert.Equal(t, "DROP TABLE users;", s.ToSQL())
	})
}

func TestPlanSummary(t *testing.T) {
	t.Run("empty plan", func(t *testing.T) {
		p := Plan{}
		counts := p.Summary()
		assert.Empty(t, counts)
	})

	t.Run("mixed actions", func(t *testing.T) {
		p := Plan{
			Statements: []Statement{
				{DDL: "CREATE TABLE a", Action: ActionCreate},
				{DDL: "CREATE TABLE b", Action: ActionCreate},
				{DDL: "DROP TABLE c", Action: ActionDestroy},
				{DDL: "ALTER TABLE d", Action: ActionUpdate},
			},
		}
		counts := p.Summary()
		assert.Equal(t, 2, counts[ActionCreate])
		assert.Equal(t, 1, counts[ActionDestroy])
		assert.Equal(t, 1, counts[ActionUpdate])
	})
}
