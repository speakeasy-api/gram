package repo

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// CHTX is the interface for executing ClickHouse queries and commands.
// It matches the subset of methods we use from clickhouse.Conn.
type CHTX interface {
	Exec(ctx context.Context, query string, args ...interface{}) error
	Query(ctx context.Context, query string, args ...interface{}) (driver.Rows, error)
}

// Queries contains methods for executing database operations.
type Queries struct {
	conn CHTX
}

// WithConn returns a new Queries instance using the provided connection.
func (q *Queries) WithConn(conn CHTX) *Queries {
	return &Queries{
		conn: conn,
	}
}

// New creates a new Queries instance with logger and tracer.
func New(conn CHTX) *Queries {
	return &Queries{
		conn: conn,
	}
}
