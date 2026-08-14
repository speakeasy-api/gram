// Package schema models the tenancy shape of a database: which tables carry
// organization_id or project_id, which reach one through a foreign key, and
// which are global.
//
// Everything downstream consumes the Table set through the Source interface, so
// the tenancy rule never touches a file path. Reading the shape from a live
// database instead of a schema dump is a matter of adding a Source.
package schema

import (
	"context"
	"slices"
)

// Tenancy column names. These are the only two columns sqlclint treats as a
// tenant boundary.
const (
	ColumnOrganizationID = "organization_id"
	ColumnProjectID      = "project_id"
)

// Source supplies the tables sqlclint reasons about.
type Source interface {
	Tables(ctx context.Context) ([]Table, error)
}

// Column is one column of a table.
type Column struct {
	// Name is the column name as written in the schema.
	Name string

	// Nullable reports whether the column accepts NULL. A nullable tenancy
	// column cannot be a required boundary, because rows holding NULL fall out
	// of any equality predicate written against it.
	Nullable bool
}

// Ref is a foreign key from a table to another table.
type Ref struct {
	// Table is the referenced table's name.
	Table string
}

// Table is one table's tenancy-relevant shape.
type Table struct {
	// Name is the table name.
	Name string

	// Columns is every column, keyed by name.
	Columns map[string]Column

	// ForeignKeys is every table this one references, in declaration order. It
	// carries the tenancy of child tables that have no tenancy column of their
	// own.
	ForeignKeys []Ref
}

// Requirement is the set of tenancy columns that satisfy a table. Binding any
// one of them is enough. An empty Requirement means the table is global and
// imposes no tenancy obligation.
type Requirement []string

// Satisfies reports whether binding the given columns meets this requirement.
func (r Requirement) Satisfies(bound []string) bool {
	for _, c := range r {
		if slices.Contains(bound, c) {
			return true
		}
	}
	return false
}

// Global reports whether the table imposes no tenancy obligation at all.
func (r Requirement) Global() bool { return len(r) == 0 }

// Classifier answers, for any table, which tenancy columns satisfy it.
type Classifier struct {
	tables map[string]Table
	req    map[string]Requirement
}

// NewClassifier resolves every table's requirement, including the transitive
// foreign key walk for tables with no tenancy column of their own.
func NewClassifier(tables []Table) *Classifier {
	byName := make(map[string]Table, len(tables))
	for _, t := range tables {
		byName[t.Name] = t
	}

	c := &Classifier{tables: byName, req: make(map[string]Requirement, len(tables))}
	for _, t := range tables {
		c.req[t.Name] = c.resolve(t.Name, make(map[string]bool))
	}
	return c
}

// resolve computes a table's requirement.
//
// The ordering below is what makes the boundary trustworthy: the required
// column is the narrowest one guaranteed to be present. A nullable column can
// never be that, since rows holding NULL would silently escape the predicate.
func (c *Classifier) resolve(name string, visiting map[string]bool) Requirement {
	if r, done := c.req[name]; done {
		return r
	}
	// A foreign key cycle must not recurse forever. Contributing nothing is
	// correct here: any real tenancy on the cycle is still found by the other
	// branches of the walk.
	if visiting[name] {
		return nil
	}
	visiting[name] = true
	defer delete(visiting, name)

	t, ok := c.tables[name]
	if !ok {
		return nil
	}

	org, hasOrg := t.Columns[ColumnOrganizationID]
	proj, hasProj := t.Columns[ColumnProjectID]

	switch {
	case hasProj && !proj.Nullable:
		return Requirement{ColumnProjectID}
	case hasOrg && !org.Nullable:
		return Requirement{ColumnOrganizationID}
	case hasOrg && hasProj:
		// Both nullable: neither is guaranteed, so either one is accepted.
		return Requirement{ColumnOrganizationID, ColumnProjectID}
	case hasProj:
		return Requirement{ColumnProjectID}
	case hasOrg:
		return Requirement{ColumnOrganizationID}
	}

	// No tenancy column of its own. A child row's tenant is whatever its parents'
	// tenant is, so inherit the union of what the parents accept.
	var inherited Requirement
	for _, fk := range t.ForeignKeys {
		for _, col := range c.resolve(fk.Table, visiting) {
			if !slices.Contains(inherited, col) {
				inherited = append(inherited, col)
			}
		}
	}
	slices.Sort(inherited)
	return inherited
}

// Require returns the tenancy columns that satisfy a table. The second result
// is false when the table is absent from the schema, which the caller reports
// rather than treating as global.
func (c *Classifier) Require(table string) (Requirement, bool) {
	r, ok := c.req[table]
	return r, ok
}

// QueryRequirement reduces the tables a single query touches to the one
// requirement that query must meet.
//
// The narrowest boundary among the referenced tables wins. A query joining a
// project-scoped table to the organization-scoped table above it is fully bounded
// by project_id: demanding organization_id as well would reject correct SQL,
// because the join key already pins the row to that project's organization.
//
// The tradeoff runs one way only. When a query mixes a project-required table
// with an organization-required one, project_id alone satisfies it, which can
// under-select rows whose nullable project is NULL. That is a completeness bug,
// never a disclosure: a narrower bound can only ever return fewer rows than the
// caller is entitled to.
func QueryRequirement(reqs []Requirement) Requirement {
	var sawOrg, sawEither bool
	for _, r := range reqs {
		switch {
		case len(r) == 1 && r[0] == ColumnProjectID:
			return Requirement{ColumnProjectID}
		case len(r) == 1 && r[0] == ColumnOrganizationID:
			sawOrg = true
		case len(r) > 1:
			sawEither = true
		}
	}

	switch {
	case sawOrg:
		return Requirement{ColumnOrganizationID}
	case sawEither:
		return Requirement{ColumnOrganizationID, ColumnProjectID}
	default:
		return nil
	}
}
