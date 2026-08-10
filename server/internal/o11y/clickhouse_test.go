package o11y

import (
	"context"
	"errors"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

var errFakeClickhouse = errors.New("fake clickhouse error")

// fakeClickhouseConn records what reaches the inner connection. The embedded
// nil Conn panics on any call the decorator does not explicitly delegate,
// pinning the wrapper's method set. The span context is captured from the
// OTel context; the clickhouse.WithSpan attachment itself is unobservable
// from outside the driver (clickhouse-go keeps its query options behind an
// unexported context key), so these tests pin delegation and span-context
// propagation, not the option payload.
type fakeClickhouseConn struct {
	clickhouse.Conn
	gotSpanContext trace.SpanContext
	gotQuery       string
	gotArgs        []any
	rows           driver.Rows
	row            driver.Row
	err            error
}

func (f *fakeClickhouseConn) record(ctx context.Context, query string, args []any) {
	f.gotSpanContext = trace.SpanContextFromContext(ctx)
	f.gotQuery = query
	f.gotArgs = args
}

func (f *fakeClickhouseConn) Exec(ctx context.Context, query string, args ...any) error {
	f.record(ctx, query, args)
	return f.err
}

func (f *fakeClickhouseConn) Select(ctx context.Context, dest any, query string, args ...any) error {
	f.record(ctx, query, args)
	return f.err
}

func (f *fakeClickhouseConn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	f.record(ctx, query, args)
	return f.rows, f.err
}

func (f *fakeClickhouseConn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	f.record(ctx, query, args)
	return f.row
}

type fakeClickhouseRows struct{ driver.Rows }

type fakeClickhouseRow struct{ driver.Row }

// spanContext returns a context carrying a valid remote span context, without
// needing an SDK tracer.
func spanContext(t *testing.T) (context.Context, trace.SpanContext) {
	t.Helper()
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		SpanID:     trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		TraceFlags: trace.FlagsSampled,
		TraceState: trace.TraceState{},
		Remote:     false,
	})
	require.True(t, sc.IsValid())
	return trace.ContextWithSpanContext(t.Context(), sc), sc
}

func TestTraceClickhouseConn_ExecDelegatesAndForwardsSpan(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{
		Conn:           nil,
		gotSpanContext: trace.SpanContext{},
		gotQuery:       "",
		gotArgs:        nil,
		rows:           nil,
		row:            nil,
		err:            errFakeClickhouse,
	}
	conn := TraceClickhouseConn(inner)
	ctx, sc := spanContext(t)

	err := conn.Exec(ctx, "INSERT INTO widgets VALUES (?)", 7)

	require.ErrorIs(t, err, errFakeClickhouse, "driver error must pass through unchanged")
	require.Equal(t, "INSERT INTO widgets VALUES (?)", inner.gotQuery)
	require.Equal(t, []any{7}, inner.gotArgs)
	require.Equal(t, sc.TraceID(), inner.gotSpanContext.TraceID(), "caller span context must reach the driver")
}

func TestTraceClickhouseConn_SelectDelegatesAndForwardsSpan(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{
		Conn:           nil,
		gotSpanContext: trace.SpanContext{},
		gotQuery:       "",
		gotArgs:        nil,
		rows:           nil,
		row:            nil,
		err:            errFakeClickhouse,
	}
	conn := TraceClickhouseConn(inner)
	ctx, sc := spanContext(t)

	var dest []string
	err := conn.Select(ctx, &dest, "SELECT name FROM widgets WHERE id = ?", 7)

	require.ErrorIs(t, err, errFakeClickhouse)
	require.Equal(t, "SELECT name FROM widgets WHERE id = ?", inner.gotQuery)
	require.Equal(t, sc.TraceID(), inner.gotSpanContext.TraceID())
}

func TestTraceClickhouseConn_QueryDelegatesAndForwardsSpan(t *testing.T) {
	t.Parallel()

	rows := &fakeClickhouseRows{Rows: nil}
	inner := &fakeClickhouseConn{
		Conn:           nil,
		gotSpanContext: trace.SpanContext{},
		gotQuery:       "",
		gotArgs:        nil,
		rows:           rows,
		row:            nil,
		err:            nil,
	}
	conn := TraceClickhouseConn(inner)
	ctx, sc := spanContext(t)

	got, err := conn.Query(ctx, "SELECT * FROM widgets")

	require.NoError(t, err)
	require.Same(t, driver.Rows(rows), got, "rows must pass through unwrapped")
	require.Equal(t, sc.TraceID(), inner.gotSpanContext.TraceID())
}

func TestTraceClickhouseConn_QueryRowDelegatesAndForwardsSpan(t *testing.T) {
	t.Parallel()

	row := &fakeClickhouseRow{Row: nil}
	inner := &fakeClickhouseConn{
		Conn:           nil,
		gotSpanContext: trace.SpanContext{},
		gotQuery:       "",
		gotArgs:        nil,
		rows:           nil,
		row:            row,
		err:            nil,
	}
	conn := TraceClickhouseConn(inner)
	ctx, sc := spanContext(t)

	got := conn.QueryRow(ctx, "SELECT count() FROM widgets")

	require.Same(t, driver.Row(row), got, "row must pass through unwrapped")
	require.Equal(t, sc.TraceID(), inner.gotSpanContext.TraceID())
}

func TestTraceClickhouseConn_NoCallerSpanPassesContextThrough(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{
		Conn:           nil,
		gotSpanContext: trace.SpanContext{},
		gotQuery:       "",
		gotArgs:        nil,
		rows:           nil,
		row:            nil,
		err:            nil,
	}
	conn := TraceClickhouseConn(inner)

	// No span in ctx: the wrapper must not fabricate one (an invalid span
	// context forwarded to ClickHouse would corrupt its span log linkage).
	require.NoError(t, conn.Exec(t.Context(), "SELECT 1"))
	require.False(t, inner.gotSpanContext.IsValid())
}
