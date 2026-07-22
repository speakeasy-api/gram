package repo

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"go.opentelemetry.io/otel/trace"
)

// CHTX is the interface for executing ClickHouse queries and commands.
// It matches the subset of methods we use from clickhouse.Conn.
type CHTX interface {
	Exec(ctx context.Context, query string, args ...any) error
	Query(ctx context.Context, query string, args ...any) (driver.Rows, error)
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

// chQueryContext forwards the caller's OTel span context to ClickHouse so the
// server-side query spans (system.opentelemetry_span_log) join the request
// trace. clickhouse-go only sends trace context that is explicitly attached
// via clickhouse.Context/WithSpan — it never reads the span from ctx itself.
func chQueryContext(ctx context.Context) context.Context {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return clickhouse.Context(ctx, clickhouse.WithSpan(sc))
	}
	return ctx
}
