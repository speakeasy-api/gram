package chrepo

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// CHTX is the interface for executing ClickHouse queries and commands. It
// matches the subset of methods we use from clickhouse.Conn.
type CHTX interface {
	Exec(ctx context.Context, query string, args ...any) error
	Query(ctx context.Context, query string, args ...any) (driver.Rows, error)
}

// Queries contains methods for executing ClickHouse operations against the
// risk analytics tables.
type Queries struct {
	conn CHTX
}

// WithConn returns a new Queries instance using the provided connection.
func (q *Queries) WithConn(conn CHTX) *Queries {
	return &Queries{
		conn: conn,
	}
}

// New creates a new Queries instance backed by the provided connection.
func New(conn CHTX) *Queries {
	return &Queries{
		conn: conn,
	}
}
