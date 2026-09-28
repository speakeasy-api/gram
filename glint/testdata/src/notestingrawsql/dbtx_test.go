package notestingrawsql

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"notestingrawsql/repo"
)

// dbtxHolder embeds a sqlc-generated DBTX so both the explicit c.DBTX.Exec
// selector and the promoted c.Exec selector resolve to the DBTX method.
type dbtxHolder struct {
	repo.DBTX
}

// execer shares method names and parameters with DBTX but not its pgx result
// types, so calls through it must not be flagged.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) error
}

func badDBTX(ctx context.Context, db repo.DBTX, c dbtxHolder) {
	_, _ = db.Exec(ctx, "INSERT INTO foo VALUES ($1)", 1) // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`
	_, _ = db.Query(ctx, "SELECT 1")                      // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`
	_ = db.QueryRow(ctx, "SELECT 1")                      // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`

	_, _ = c.DBTX.Query(ctx, "SELECT 1")                 // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`
	_, _ = c.Exec(ctx, "INSERT INTO foo VALUES ($1)", 1) // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`
}

func goodNonPgxInterfaces(ctx context.Context, ch driver.Conn, e execer) {
	_ = ch.Exec(ctx, "INSERT INTO foo VALUES (?)", 1)
	_, _ = ch.Query(ctx, "SELECT 1")
	_ = ch.QueryRow(ctx, "SELECT 1")

	_ = e.Exec(ctx, "anything")
}
