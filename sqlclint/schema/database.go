package schema

import (
	"context"
	"errors"
)

// ErrDatabaseSourceUnimplemented is returned by DatabaseSource until it is
// built out.
var ErrDatabaseSourceUnimplemented = errors.New("schema: reading the tenancy shape from a live database is not implemented yet; pass --schema-file")

// DatabaseSource reads the tenancy shape from a live Postgres database rather
// than a schema dump, for callers linting against a deployed database.
//
// It is declared so the Source abstraction has a second implementation to keep
// it honest; the query work is not done.
//
// TODO(sqlclint): populate Table from information_schema.columns (name,
// is_nullable) joined against pg_constraint (contype = 'f') for the foreign
// keys, then drop ErrDatabaseSourceUnimplemented.
type DatabaseSource struct {
	dsn string
}

var _ Source = (*DatabaseSource)(nil)

// NewDatabaseSource returns a Source backed by the database at dsn.
func NewDatabaseSource(dsn string) *DatabaseSource {
	return &DatabaseSource{dsn: dsn}
}

// Tables reports ErrDatabaseSourceUnimplemented.
func (s *DatabaseSource) Tables(ctx context.Context) ([]Table, error) {
	return nil, ErrDatabaseSourceUnimplemented
}
