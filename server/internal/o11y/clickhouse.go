package o11y

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"go.opentelemetry.io/otel/trace"
)

// tracedClickhouseConn forwards the caller's OTel span context to ClickHouse
// on every Query, QueryRow, Select, and Exec call, so the server-side
// execution spans ClickHouse records in system.opentelemetry_span_log carry
// the request's trace id and can be joined against APM traces during an
// investigation. clickhouse-go only sends trace context that is explicitly
// attached via clickhouse.Context/WithSpan; it never reads the span from ctx
// itself.
//
// Forwarding is deliberately the wrapper's whole job: client-side spans and a
// per-query duration metric were tried and removed (DNO-602) — per-query
// latency is investigated in ClickHouse's own query_log/span_log by trace id,
// and service-level latency comes from the ClickHouse Cloud Datadog
// integration. PrepareBatch and AsyncInsert pass through untouched.
type tracedClickhouseConn struct {
	clickhouse.Conn
}

// TraceClickhouseConn wraps conn so every call forwards the caller's span
// context to ClickHouse. Wrap once at connection creation (cmd/gram's
// newClickhouseClient); everything built on the connection inherits it.
func TraceClickhouseConn(conn clickhouse.Conn) clickhouse.Conn {
	return &tracedClickhouseConn{Conn: conn}
}

// withCallerSpan attaches the caller's span context for ClickHouse-side
// tracing. clickhouse-go v2 merges: Context() seeds from the parent's
// existing QueryOptions before applying WithSpan, so caller options
// (WithAsync, settings, parameters) survive this wrap. That merge is driver
// behavior, not contract — re-verify on driver upgrades.
func withCallerSpan(ctx context.Context) context.Context {
	if sc := trace.SpanFromContext(ctx).SpanContext(); sc.IsValid() {
		return clickhouse.Context(ctx, clickhouse.WithSpan(sc))
	}
	return ctx
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (c *tracedClickhouseConn) Exec(ctx context.Context, query string, args ...any) error {
	return c.Conn.Exec(withCallerSpan(ctx), query, args...)
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (c *tracedClickhouseConn) Select(ctx context.Context, dest any, query string, args ...any) error {
	return c.Conn.Select(withCallerSpan(ctx), dest, query, args...)
}

func (c *tracedClickhouseConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	return c.Conn.QueryRow(withCallerSpan(ctx), query, args...)
}

//nolint:wrapcheck // A transparent decorator must return the driver's error unchanged.
func (c *tracedClickhouseConn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	return c.Conn.Query(withCallerSpan(ctx), query, args...)
}
