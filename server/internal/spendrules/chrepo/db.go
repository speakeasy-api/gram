package chrepo

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// CHTX matches the subset of clickhouse.Conn methods used here.
type CHTX interface {
	Query(ctx context.Context, query string, args ...any) (driver.Rows, error)
}

// Queries holds the ClickHouse connection used to read spend-rule rollups.
type Queries struct {
	conn CHTX
}

// New builds a Queries bound to the given ClickHouse connection.
func New(conn CHTX) *Queries {
	return &Queries{conn: conn}
}
