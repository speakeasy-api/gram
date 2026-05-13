// Package repo writes authz challenge rows to ClickHouse.
//
// Inserts use server-side async insert (async_insert=1,
// wait_for_async_insert=0) so ClickHouse buffers rows and responds
// immediately without waiting for a disk flush. The Exec call itself
// still blocks on the network roundtrip to CH.
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
