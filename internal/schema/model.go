package schema

// Schema represents the public schema of a DSQL database.
type Schema struct {
	Tables []Table
}

// Table represents a database table with its columns, constraints, and indexes.
type Table struct {
	Name              string
	Columns           []Column
	PrimaryKey        *PrimaryKey
	UniqueConstraints []UniqueConstraint
	CheckConstraints  []CheckConstraint
	Indexes           []Index
}

// Column represents a table column.
type Column struct {
	Name               string
	Type               string // normalized: TEXT, BOOLEAN, INTEGER, etc.
	IsNullable         bool
	Default            string // raw default expression, empty if none
	IsIdentity         bool
	IdentityGeneration string // "ALWAYS" or "BY DEFAULT", empty if not identity
	OrdinalPosition    int
}

// PrimaryKey represents a PRIMARY KEY constraint.
type PrimaryKey struct {
	Name    string
	Columns []string
}

// UniqueConstraint represents a UNIQUE constraint.
type UniqueConstraint struct {
	Name    string
	Columns []string
}

// CheckConstraint represents a CHECK constraint.
type CheckConstraint struct {
	Name       string
	Expression string // e.g. "(status = ANY (ARRAY['open'::text, 'done'::text]))"
}

// Index represents a btree index (the only type DSQL supports).
type Index struct {
	Name           string
	Columns        []string
	IncludeColumns []string
	IsUnique       bool
}
