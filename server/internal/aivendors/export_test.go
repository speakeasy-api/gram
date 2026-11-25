package aivendors

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LoadWithRemovedCap reads the catalog keeping at most removedCap removed documents.
func LoadWithRemovedCap(ctx context.Context, db *pgxpool.Pool, validate func(*Snapshot) error, removedCap int64) (*Snapshot, []error, error) {
	return load(ctx, db, validate, removedCap)
}
