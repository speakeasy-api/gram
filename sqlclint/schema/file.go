package schema

import (
	"context"
	"fmt"
	"os"

	pganalyze "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
)

// FileSource reads the tenancy shape from a schema dump such as
// server/database/schema.sql.
//
// It parses the file with libpg_query rather than scanning it, so NOT NULL and
// REFERENCES are read from the grammar instead of inferred from formatting.
type FileSource struct {
	path string
}

var _ Source = (*FileSource)(nil)

// NewFileSource returns a Source backed by the schema file at path.
func NewFileSource(path string) *FileSource {
	return &FileSource{path: path}
}

// Tables parses the schema file and returns every CREATE TABLE it declares.
func (s *FileSource) Tables(ctx context.Context) ([]Table, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read schema file: %w", err)
	}

	tree, err := pgquery.Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse schema file %s: %w", s.path, err)
	}

	var out []Table
	for _, stmt := range tree.GetStmts() {
		create := stmt.GetStmt().GetCreateStmt()
		if create == nil {
			continue
		}
		out = append(out, tableFromCreateStmt(create))
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("schema file %s declares no tables", s.path)
	}
	return out, nil
}

// tableFromCreateStmt extracts the columns and foreign keys sqlclint needs.
func tableFromCreateStmt(create *pganalyze.CreateStmt) Table {
	t := Table{
		Name:        create.GetRelation().GetRelname(),
		Columns:     map[string]Column{},
		ForeignKeys: nil,
	}

	for _, el := range create.GetTableElts() {
		if col := el.GetColumnDef(); col != nil {
			t.Columns[col.GetColname()] = Column{
				Name:     col.GetColname(),
				Nullable: columnNullable(col),
			}

			// REFERENCES written inline on the column.
			for _, c := range col.GetConstraints() {
				if ref := foreignKeyTarget(c.GetConstraint()); ref != "" {
					t.ForeignKeys = append(t.ForeignKeys, Ref{Table: ref})
				}
			}
			continue
		}

		// FOREIGN KEY written as a separate table constraint.
		if ref := foreignKeyTarget(el.GetConstraint()); ref != "" {
			t.ForeignKeys = append(t.ForeignKeys, Ref{Table: ref})
		}
	}

	return t
}

// columnNullable reports whether a column accepts NULL.
//
// Postgres columns are nullable by default, so this looks for what removes that:
// an explicit NOT NULL, or a PRIMARY KEY constraint, which implies it.
func columnNullable(col *pganalyze.ColumnDef) bool {
	if col.GetIsNotNull() {
		return false
	}
	for _, c := range col.GetConstraints() {
		switch c.GetConstraint().GetContype() {
		case pganalyze.ConstrType_CONSTR_NOTNULL, pganalyze.ConstrType_CONSTR_PRIMARY:
			return false
		}
	}
	return true
}

// foreignKeyTarget returns the referenced table name for a FOREIGN KEY
// constraint, or "" for any other constraint.
func foreignKeyTarget(c *pganalyze.Constraint) string {
	if c == nil || c.GetContype() != pganalyze.ConstrType_CONSTR_FOREIGN {
		return ""
	}
	return c.GetPktable().GetRelname()
}
