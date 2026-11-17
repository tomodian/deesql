package schema

// Schema represents the public schema of a DSQL database.
type Schema struct {
	Tables    []Table
	Sequences []Sequence
}

// Table represents a database table with its columns, constraints, and indexes.
type Table struct {
	Name              string
	Columns           []Column
	PrimaryKey        *PrimaryKey
	UniqueConstraints []UniqueConstraint
	CheckConstraints  []CheckConstraint
	ForeignKeys       []ForeignKey
	Indexes           []Index
	Statistics        []Statistics
}

// Column represents a table column.
type Column struct {
	Name               string
	Type               string // normalized: TEXT, BOOLEAN, INTEGER, etc.
	IsNullable         bool
	Default            string // raw default expression, empty if none
	IsIdentity         bool
	IdentityGeneration string // "ALWAYS" or "BY DEFAULT", empty if not identity
	IdentityOptions    string // raw sequence options, e.g. "CACHE 70000" (not diffed)
	Generated          string // stored generation expression, e.g. "(a + b)", empty if none
	OrdinalPosition    int
}

// PrimaryKey represents a PRIMARY KEY constraint.
type PrimaryKey struct {
	Name    string
	Columns []string
}

// UniqueConstraint represents a UNIQUE constraint.
type UniqueConstraint struct {
	Name             string
	Columns          []string
	NullsNotDistinct bool
}

// CheckConstraint represents a CHECK constraint.
type CheckConstraint struct {
	Name       string
	Expression string // e.g. "(status = ANY (ARRAY['open'::text, 'done'::text]))"
	NotValid   bool   // live constraint exists but has not been validated
}

// ForeignKey represents a FOREIGN KEY constraint.
type ForeignKey struct {
	Name              string
	Columns           []string
	RefTable          string
	RefColumns        []string
	OnDelete          string // "" (NO ACTION), "RESTRICT", "CASCADE", "SET NULL", "SET DEFAULT"
	OnUpdate          string // same values as OnDelete
	MatchFull         bool
	Deferrable        bool
	InitiallyDeferred bool
	NotValid          bool // live constraint exists but has not been validated
}

// Index represents a btree index (the only type DSQL supports).
type Index struct {
	Name             string
	Columns          []string // key elements: column names or expressions, optionally with NULLS FIRST/LAST
	IncludeColumns   []string
	IsUnique         bool
	NullsNotDistinct bool
	Where            string // partial index predicate, empty if none
}

// Statistics represents an extended statistics object (CREATE STATISTICS).
type Statistics struct {
	Name        string
	Kinds       []string // ndistinct, dependencies, mcv (empty means all)
	Expressions []string // columns or parenthesized expressions
}

// Sequence represents a standalone sequence (not backing an identity column).
// Values are the effective settings after PostgreSQL defaults are applied.
type Sequence struct {
	Name      string
	Increment int64
	MinValue  int64
	MaxValue  int64
	Start     int64
	Cache     int64
	Cycle     bool
	OwnedBy   string // "table.column", only applied on create (not diffed)
}
