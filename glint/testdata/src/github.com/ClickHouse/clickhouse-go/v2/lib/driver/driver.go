package driver

import "context"

type Rows interface {
	Next() bool
}

type Row interface {
	Scan(dest ...any) error
}

// Conn is a minimal stand-in for
// github.com/ClickHouse/clickhouse-go/v2/lib/driver.Conn, whose Exec, Query,
// and QueryRow method names collide with pgx but must not be flagged by the
// notestingrawsql analyzer.
type Conn interface {
	Exec(ctx context.Context, query string, args ...any) error
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) Row
}
