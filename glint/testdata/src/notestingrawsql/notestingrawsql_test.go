package notestingrawsql

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// nonPgxStruct ensures the analyzer ignores method calls on receivers that are
// not pgx types, even when the method names collide with the watched set.
type nonPgxStruct struct{}

func (nonPgxStruct) Exec(_ context.Context, _ string, _ ...any) {}

func bad(ctx context.Context, pool *pgxpool.Pool, conn *pgx.Conn, tx pgx.Tx, q pgx.Querier) {
	_, _ = pool.Exec(ctx, "INSERT INTO foo VALUES ($1)", 1)            // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`
	_, _ = pool.Query(ctx, "SELECT 1")                                 // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`
	_ = pool.QueryRow(ctx, "SELECT 1")                                 // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`
	_, _ = pool.Begin(ctx)                                             // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`
	_, _ = pool.BeginTx(ctx, pgx.TxOptions{})                          // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`
	_ = pool.SendBatch(ctx, &pgx.Batch{})                              // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`
	_, _ = pool.CopyFrom(ctx, pgx.Identifier{"t"}, []string{"c"}, nil) // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`

	_, _ = conn.Exec(ctx, "INSERT INTO foo VALUES ($1)", 1) // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`
	_ = conn.SendBatch(ctx, &pgx.Batch{})                   // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`

	_, _ = tx.Exec(ctx, "INSERT INTO foo VALUES ($1)", 1)            // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`
	_, _ = tx.CopyFrom(ctx, pgx.Identifier{"t"}, []string{"c"}, nil) // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`

	_, _ = q.Exec(ctx, "INSERT INTO foo VALUES ($1)", 1) // want `use SQLc-generated methods from the relevant package's queries.sql \(or testenv/testrepo for fixtures genuinely shared across packages\)`

	// Non-pgx receivers with matching method names must NOT be flagged.
	(nonPgxStruct{}).Exec(ctx, "anything")
}
