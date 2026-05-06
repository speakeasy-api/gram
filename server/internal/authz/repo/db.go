// Package chrepo writes authz challenge rows to ClickHouse.
//
// Inserts use server-side async insert (clickhouse.WithAsync(true) +
// wait_for_async_insert=0) so the request path never blocks on the network or
// on CH batch construction. CH buffers and flushes batches itself.
package repo

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// CHTX matches the subset of clickhouse.Conn methods used here.
type CHTX interface {
	Exec(ctx context.Context, query string, args ...any) error
	Query(ctx context.Context, query string, args ...any) (driver.Rows, error)
}

// Queries holds the ClickHouse connection used to write challenge rows.
type Queries struct {
	conn CHTX
}

// New builds a Queries bound to the given ClickHouse connection.
func New(conn CHTX) *Queries {
	return &Queries{conn: conn}
}
