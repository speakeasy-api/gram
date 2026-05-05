package notestingrawsql

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// production code is not flagged — the rule only fires inside *_test.go files.
func ProductionInsert(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, "INSERT INTO foo VALUES ($1)", 1)
}
