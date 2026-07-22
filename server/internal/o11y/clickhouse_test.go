package o11y

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type fakeClickhouseRows struct {
	driver.Rows
	closeErr error
	closed   bool
}

func (r *fakeClickhouseRows) Close() error {
	r.closed = true
	return r.closeErr
}

// fakeClickhouseConn captures the context and query each call receives and
// returns the configured results. Only the traced methods are implemented;
// everything else panics via the embedded nil interface if reached.
type fakeClickhouseConn struct {
	clickhouse.Conn
	execErr   error
	queryErr  error
	selectErr error
	rows      *fakeClickhouseRows

	gotSpanContext trace.SpanContext
	gotQuery       string
}

func (f *fakeClickhouseConn) Exec(ctx context.Context, query string, _ ...any) error {
	f.gotSpanContext = trace.SpanContextFromContext(ctx)
	f.gotQuery = query
	return f.execErr
}

func (f *fakeClickhouseConn) Select(ctx context.Context, _ any, query string, _ ...any) error {
	f.gotSpanContext = trace.SpanContextFromContext(ctx)
	f.gotQuery = query
	return f.selectErr
}

func (f *fakeClickhouseConn) Query(ctx context.Context, query string, _ ...any) (driver.Rows, error) {
	f.gotSpanContext = trace.SpanContextFromContext(ctx)
	f.gotQuery = query
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.rows, nil
}

func newRecordingTracedConn(t *testing.T, inner clickhouse.Conn) (clickhouse.Conn, *tracetest.SpanRecorder) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	meterProvider := sdkmetric.NewMeterProvider()
	t.Cleanup(func() {
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})
	// testenv.NewLogger is unavailable here: testenv imports o11y, so the
	// in-package test would cycle. Nothing asserts on logs.
	return TraceClickhouseConn(inner, tracerProvider, meterProvider, slog.New(slog.DiscardHandler)), recorder //nolint:forbidigo // GG006: testenv imports o11y; using it here would be an import cycle.
}

func requireSpanAttr(t *testing.T, span sdktrace.ReadOnlySpan, key, want string) {
	t.Helper()
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			require.Equal(t, want, kv.Value.AsString())
			return
		}
	}
	require.Failf(t, "missing span attribute", "span %q has no attribute %q", span.Name(), key)
}

func TestTraceClickhouseConn_ExecEmitsSpanWithQueryText(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, recorder := newRecordingTracedConn(t, inner)

	require.NoError(t, conn.Exec(t.Context(), "ALTER TABLE chat_session_summaries DELETE WHERE 1"))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "clickhouse.exec", spans[0].Name())
	require.Equal(t, trace.SpanKindClient, spans[0].SpanKind())
	require.Equal(t, codes.Unset, spans[0].Status().Code)
	requireSpanAttr(t, spans[0], "db.query.text", "ALTER TABLE chat_session_summaries DELETE WHERE 1")
	requireSpanAttr(t, spans[0], "gram.clickhouse.table", "chat_session_summaries")

	// The inner call must receive the client span's context so ClickHouse's
	// server-side spans parent under it.
	require.True(t, inner.gotSpanContext.IsValid())
	require.Equal(t, spans[0].SpanContext().SpanID(), inner.gotSpanContext.SpanID())
}

func TestTraceClickhouseConn_ExecRecordsError(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{execErr: errors.New("boom")}
	conn, recorder := newRecordingTracedConn(t, inner)

	require.Error(t, conn.Exec(t.Context(), "ALTER TABLE t DELETE WHERE 1"))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.NotEmpty(t, spans[0].Events(), "error must be recorded on the span")
}

func TestTraceClickhouseConn_SelectEmitsSpan(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, recorder := newRecordingTracedConn(t, inner)

	var dest []struct{}
	require.NoError(t, conn.Select(t.Context(), &dest, "SELECT 1 FROM trace_summaries"))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "clickhouse.select", spans[0].Name())
	requireSpanAttr(t, spans[0], "gram.clickhouse.table", "trace_summaries")
}

func TestTraceClickhouseConn_QuerySpanEndsOnRowsClose(t *testing.T) {
	t.Parallel()

	rows := &fakeClickhouseRows{}
	inner := &fakeClickhouseConn{rows: rows}
	conn, recorder := newRecordingTracedConn(t, inner)

	got, err := conn.Query(t.Context(), "SELECT chat_id FROM chat_session_summaries")
	require.NoError(t, err)
	// ClickHouse streams results, so the span must stay open until the
	// caller finishes consuming them.
	require.Empty(t, recorder.Ended(), "query span must not end before rows are closed")

	require.NoError(t, got.Close())
	require.True(t, rows.closed)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "clickhouse.query", spans[0].Name())
	requireSpanAttr(t, spans[0], "db.query.text", "SELECT chat_id FROM chat_session_summaries")
	requireSpanAttr(t, spans[0], "gram.clickhouse.table", "chat_session_summaries")
}

func TestTraceClickhouseConn_QueryErrorEndsSpanWithError(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{queryErr: errors.New("no such table")}
	conn, recorder := newRecordingTracedConn(t, inner)

	_, err := conn.Query(t.Context(), "SELECT broken")
	require.Error(t, err)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
}

func TestTraceClickhouseConn_RowsCloseErrorMarksSpan(t *testing.T) {
	t.Parallel()

	rows := &fakeClickhouseRows{closeErr: errors.New("connection reset")}
	inner := &fakeClickhouseConn{rows: rows}
	conn, recorder := newRecordingTracedConn(t, inner)

	got, err := conn.Query(t.Context(), "SELECT 1")
	require.NoError(t, err)
	require.Error(t, got.Close())

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
}

func TestClickhouseTargetTable(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"SELECT chat_id FROM chat_session_summaries s WHERE 1":   "chat_session_summaries",
		"SELECT * FROM (SELECT * FROM telemetry_logs) WHERE 1":   "telemetry_logs",
		"WITH x AS (SELECT 1) SELECT * FROM trace_summaries":     "trace_summaries",
		"INSERT INTO telemetry_logs (a, b) VALUES (?, ?)":        "telemetry_logs",
		"INSERT INTO `chat_token_summaries` (a) VALUES (?)":      "chat_token_summaries",
		"ALTER TABLE attribute_metrics_summaries DELETE WHERE 1": "attribute_metrics_summaries",
		"select lower.case from lowercase_table":                 "lowercase_table",
		"SELECT 1":                                               "unknown",
		// Pinned label limits: first FROM wins, JOINs label the leading table.
		"WITH c AS (SELECT x FROM table_a) SELECT y FROM table_b JOIN c": "table_a",
		"SELECT a.x FROM table_a a JOIN table_b b ON a.id = b.id":        "table_a",
	}
	for query, want := range cases {
		require.Equal(t, want, clickhouseTargetTable(query), "query: %s", query)
	}
}
