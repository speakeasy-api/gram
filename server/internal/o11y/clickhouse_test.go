package o11y

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type fakeClickhouseRows struct {
	driver.Rows
	closeErr  error
	closed    bool
	nextCalls int
}

// Next reports one row then exhaustion.
func (r *fakeClickhouseRows) Next() bool {
	r.nextCalls++
	return r.nextCalls <= 1
}

func (r *fakeClickhouseRows) Err() error { return nil }

func (r *fakeClickhouseRows) Close() error {
	r.closed = true
	return r.closeErr
}

// fakeClickhouseConn captures the span context and query each call receives
// and returns the configured results. Only the traced methods are
// implemented; everything else panics via the embedded nil interface if
// reached.
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

type fakeClickhouseRow struct {
	driver.Row
	scanErr error
}

func (r *fakeClickhouseRow) Scan(...any) error { return r.scanErr }

func (r *fakeClickhouseRow) Err() error { return nil }

func (f *fakeClickhouseConn) QueryRow(ctx context.Context, query string, _ ...any) driver.Row {
	f.gotSpanContext = trace.SpanContextFromContext(ctx)
	f.gotQuery = query
	return &fakeClickhouseRow{scanErr: f.queryErr}
}

func (f *fakeClickhouseConn) Query(ctx context.Context, query string, _ ...any) (driver.Rows, error) {
	f.gotSpanContext = trace.SpanContextFromContext(ctx)
	f.gotQuery = query
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.rows, nil
}

func newRecordingTracedConn(t *testing.T, inner clickhouse.Conn) (clickhouse.Conn, *tracetest.SpanRecorder, *sdkmetric.ManualReader) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})
	// testenv.NewLogger is unavailable here: testenv imports o11y, so the
	// in-package test would cycle. Nothing asserts on logs.
	return TraceClickhouseConn(inner, tracerProvider, meterProvider, slog.New(slog.DiscardHandler)), recorder, reader //nolint:forbidigo // GG006: testenv imports o11y; using it here would be an import cycle.
}

func requireSingleSpan(t *testing.T, recorder *tracetest.SpanRecorder) sdktrace.ReadOnlySpan {
	t.Helper()
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	return spans[0]
}

func requireSingleDataPoint(t *testing.T, reader *sdkmetric.ManualReader) metricdata.HistogramDataPoint[float64] {
	t.Helper()
	dps := queryDurationDataPoints(t, reader)
	require.Len(t, dps, 1)
	return dps[0]
}

func spanAttr(span sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

func requireSpanAttr(t *testing.T, span sdktrace.ReadOnlySpan, key, want string) {
	t.Helper()
	got, ok := spanAttr(span, key)
	require.True(t, ok, "span %q has no attribute %q", span.Name(), key)
	require.Equal(t, want, got)
}

// queryDurationDataPoints collects the clickhouse.client.query.duration
// histogram's data points, or an empty slice when nothing was recorded yet.
func queryDurationDataPoints(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.HistogramDataPoint[float64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != meterClickhouseQueryDuration {
				continue
			}
			require.Equal(t, "s", m.Unit)
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok, "expected a float64 histogram")
			return hist.DataPoints
		}
	}
	return nil
}

func dataPointAttr(dp metricdata.HistogramDataPoint[float64], key string) string {
	if v, ok := dp.Attributes.Value(attribute.Key(key)); ok {
		return v.AsString()
	}
	return ""
}

func TestTraceClickhouseConn_ExecEmitsSpan(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, recorder, _ := newRecordingTracedConn(t, inner)

	require.NoError(t, conn.Exec(t.Context(), "ALTER TABLE chat_session_summaries DELETE WHERE 1"))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "clickhouse.exec", spans[0].Name())
	require.Equal(t, trace.SpanKindClient, spans[0].SpanKind())
	require.Equal(t, codes.Unset, spans[0].Status().Code)
	requireSpanAttr(t, spans[0], "gram.clickhouse.table", "chat_session_summaries")
	requireSpanAttr(t, spans[0], "gram.clickhouse.operation", "TestTraceClickhouseConn_ExecEmitsSpan")

	// The inner call must receive the client span's context so ClickHouse's
	// server-side spans parent under it, and the query untouched.
	require.True(t, inner.gotSpanContext.IsValid())
	require.Equal(t, spans[0].SpanContext().SpanID(), inner.gotSpanContext.SpanID())
	require.Equal(t, "ALTER TABLE chat_session_summaries DELETE WHERE 1", inner.gotQuery)
}

// TestTraceClickhouseConn_NoQueryTextOnSpan pins a deliberate decision: the
// full SQL text is NOT attached to spans. The operation label identifies the
// function whose code builds the query, and omitting the text keeps span
// ingest volume flat even for multi-kilobyte statements.
func TestTraceClickhouseConn_NoQueryTextOnSpan(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, recorder, _ := newRecordingTracedConn(t, inner)

	require.NoError(t, conn.Exec(t.Context(), "SELECT big FROM telemetry_logs WHERE x = ?"))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	_, ok := spanAttr(spans[0], "db.query.text")
	require.False(t, ok, "spans must not carry the SQL text")
}

func TestTraceClickhouseConn_ExecRecordsError(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{execErr: errors.New("boom")}
	conn, recorder, reader := newRecordingTracedConn(t, inner)

	require.Error(t, conn.Exec(t.Context(), "ALTER TABLE t DELETE WHERE 1"))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)
	require.NotEmpty(t, spans[0].Events(), "error must be recorded on the span")

	dps := queryDurationDataPoints(t, reader)
	require.Len(t, dps, 1)
	require.Equal(t, "failure", dataPointAttr(dps[0], "gram.outcome"))
}

func TestTraceClickhouseConn_SelectEmitsSpan(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, recorder, _ := newRecordingTracedConn(t, inner)

	var dest []struct{}
	require.NoError(t, conn.Select(t.Context(), &dest, "SELECT 1 FROM trace_summaries"))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "clickhouse.select", spans[0].Name())
	requireSpanAttr(t, spans[0], "gram.clickhouse.table", "trace_summaries")
	requireSpanAttr(t, spans[0], "gram.clickhouse.operation", "TestTraceClickhouseConn_SelectEmitsSpan")
}

func TestTraceClickhouseConn_QuerySpanAndMetricCompleteOnRowsClose(t *testing.T) {
	t.Parallel()

	rows := &fakeClickhouseRows{}
	inner := &fakeClickhouseConn{rows: rows}
	conn, recorder, reader := newRecordingTracedConn(t, inner)

	got, err := conn.Query(t.Context(), "SELECT chat_id FROM chat_session_summaries")
	require.NoError(t, err)
	// ClickHouse streams results, so both the span and the duration metric
	// must wait for the caller to finish consuming them.
	require.Empty(t, recorder.Ended(), "query span must not end before rows are closed")
	require.Empty(t, queryDurationDataPoints(t, reader), "duration must not be recorded before rows are closed")

	require.NoError(t, got.Close())
	require.True(t, rows.closed)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "clickhouse.query", spans[0].Name())
	requireSpanAttr(t, spans[0], "gram.clickhouse.table", "chat_session_summaries")

	dps := queryDurationDataPoints(t, reader)
	require.Len(t, dps, 1)
	require.Equal(t, uint64(1), dps[0].Count)
	require.Equal(t, "success", dataPointAttr(dps[0], "gram.outcome"))
	require.Equal(t, "chat_session_summaries", dataPointAttr(dps[0], "gram.clickhouse.table"))
}

func TestTraceClickhouseConn_QueryErrorEndsSpanWithError(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{queryErr: errors.New("no such table")}
	conn, recorder, reader := newRecordingTracedConn(t, inner)

	_, err := conn.Query(t.Context(), "SELECT broken")
	require.Error(t, err)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)

	dps := queryDurationDataPoints(t, reader)
	require.Len(t, dps, 1)
	require.Equal(t, "failure", dataPointAttr(dps[0], "gram.outcome"))
}

func TestTraceClickhouseConn_RowsCloseErrorMarksSpan(t *testing.T) {
	t.Parallel()

	rows := &fakeClickhouseRows{closeErr: errors.New("connection reset")}
	inner := &fakeClickhouseConn{rows: rows}
	conn, recorder, reader := newRecordingTracedConn(t, inner)

	got, err := conn.Query(t.Context(), "SELECT 1")
	require.NoError(t, err)
	require.Error(t, got.Close())

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status().Code)

	dps := queryDurationDataPoints(t, reader)
	require.Len(t, dps, 1)
	require.Equal(t, "failure", dataPointAttr(dps[0], "gram.outcome"))
}

func TestTraceClickhouseConn_TimeoutOutcome(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{execErr: context.DeadlineExceeded}
	conn, _, reader := newRecordingTracedConn(t, inner)

	require.Error(t, conn.Exec(t.Context(), "SELECT 1"))

	dps := queryDurationDataPoints(t, reader)
	require.Len(t, dps, 1)
	require.Equal(t, "timeout", dataPointAttr(dps[0], "gram.outcome"))
}

// ---- Operation-name derivation ------------------------------------------

// fakeRepo mirrors the real call shape: a repository type whose methods
// issue queries through the shared connection.
type fakeRepo struct {
	conn clickhouse.Conn
}

func (r *fakeRepo) ListWidgets(ctx context.Context) error {
	if err := r.conn.Exec(ctx, "SELECT 1 FROM widgets"); err != nil {
		return fmt.Errorf("list widgets: %w", err)
	}
	return nil
}

func execViaHelper(ctx context.Context, conn clickhouse.Conn) error {
	if err := conn.Exec(ctx, "SELECT 1"); err != nil {
		return fmt.Errorf("exec via helper: %w", err)
	}
	return nil
}

func execOuterHelper(ctx context.Context, conn clickhouse.Conn) error {
	return execViaHelper(ctx, conn)
}

func TestClickhouseOperation_RepoMethod(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, recorder, reader := newRecordingTracedConn(t, inner)

	repo := &fakeRepo{conn: conn}
	require.NoError(t, repo.ListWidgets(t.Context()))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	// Pointer-receiver methods clean up to Type.Method.
	requireSpanAttr(t, spans[0], "gram.clickhouse.operation", "fakeRepo.ListWidgets")

	dps := queryDurationDataPoints(t, reader)
	require.Len(t, dps, 1)
	require.Equal(t, "fakeRepo.ListWidgets", dataPointAttr(dps[0], "gram.clickhouse.operation"))
}

func TestClickhouseOperation_PlainFunction(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, recorder, _ := newRecordingTracedConn(t, inner)

	require.NoError(t, execViaHelper(t.Context(), conn))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	requireSpanAttr(t, spans[0], "gram.clickhouse.operation", "execViaHelper")
}

func TestClickhouseOperation_NestedHelpersNameInnermostCaller(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, recorder, _ := newRecordingTracedConn(t, inner)

	require.NoError(t, execOuterHelper(t.Context(), conn))

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	// The FIRST frame outside the decorator wins: the innermost caller, not
	// the outer orchestration. Pinned so a filter change cannot silently
	// shift every label up or down the stack.
	requireSpanAttr(t, spans[0], "gram.clickhouse.operation", "execViaHelper")
}

func TestClickhouseOperation_Closure(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, recorder, _ := newRecordingTracedConn(t, inner)

	issue := func() error {
		return conn.Exec(t.Context(), "SELECT 1")
	}
	require.NoError(t, issue())

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	op, ok := spanAttr(spans[0], "gram.clickhouse.operation")
	require.True(t, ok)
	// Anonymous functions carry their parent's name plus a funcN suffix —
	// still attributable, still bounded cardinality.
	require.True(t, strings.HasPrefix(op, "TestClickhouseOperation_Closure.func"), "got %q", op)
}

func TestClickhouseOperation_Goroutine(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, recorder, _ := newRecordingTracedConn(t, inner)

	done := make(chan error, 1)
	go func() {
		done <- conn.Exec(t.Context(), "SELECT 1")
	}()
	require.NoError(t, <-done)

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	op, ok := spanAttr(spans[0], "gram.clickhouse.operation")
	require.True(t, ok)
	// A goroutine's stack roots at its closure: attribution survives the
	// goroutine boundary instead of degrading to "unknown".
	require.True(t, strings.HasPrefix(op, "TestClickhouseOperation_Goroutine.func"), "got %q", op)
}

func TestClickhouseOperation_DistinctSeriesPerOperation(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, _, reader := newRecordingTracedConn(t, inner)

	repo := &fakeRepo{conn: conn}
	require.NoError(t, repo.ListWidgets(t.Context()))
	require.NoError(t, execViaHelper(t.Context(), conn))
	require.NoError(t, execViaHelper(t.Context(), conn))

	dps := queryDurationDataPoints(t, reader)
	counts := map[string]uint64{}
	for _, dp := range dps {
		counts[dataPointAttr(dp, "gram.clickhouse.operation")] += dp.Count
	}
	require.Equal(t, map[string]uint64{
		"fakeRepo.ListWidgets": 1,
		"execViaHelper":        2,
	}, counts, "each operation must be its own metric series")
}

// ---- Table extraction ----------------------------------------------------

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
		// Database-qualified names label the TABLE segment, not the database.
		"SELECT 1 FROM system.opentelemetry_span_log":    "opentelemetry_span_log",
		"INSERT INTO gram.telemetry_logs (a) VALUES (?)": "telemetry_logs",
	}
	for query, want := range cases {
		require.Equal(t, want, clickhouseTargetTable(query), "query: %s", query)
	}
}

func TestTraceClickhouseConn_DoubleCloseRecordsOnce(t *testing.T) {
	t.Parallel()

	rows := &fakeClickhouseRows{}
	inner := &fakeClickhouseConn{rows: rows}
	conn, recorder, reader := newRecordingTracedConn(t, inner)

	got, err := conn.Query(t.Context(), "SELECT 1 FROM t")
	require.NoError(t, err)
	require.NoError(t, got.Close())
	// The explicit-Close-plus-deferred-Close pattern is legal with
	// clickhouse-go; the wrapper must not double-record.
	require.NoError(t, got.Close())

	require.Len(t, recorder.Ended(), 1)
	require.Equal(t, uint64(1), requireSingleDataPoint(t, reader).Count)
}

func TestTraceClickhouseConn_RowsExhaustionCompletesWithoutClose(t *testing.T) {
	t.Parallel()

	rows := &fakeClickhouseRows{}
	inner := &fakeClickhouseConn{rows: rows}
	conn, recorder, reader := newRecordingTracedConn(t, inner)

	got, err := conn.Query(t.Context(), "SELECT 1 FROM t")
	require.NoError(t, err)
	for got.Next() { //nolint:revive // consuming the stream is the point
	}
	// clickhouse-go releases the stream when Next returns false, and callers
	// may legally skip Close after exhaustion — the span and metric must not
	// leak open in that pattern.
	require.Len(t, recorder.Ended(), 1)
	require.Equal(t, uint64(1), requireSingleDataPoint(t, reader).Count)

	// A later deferred Close stays a no-op for recording.
	require.NoError(t, got.Close())
	require.Len(t, recorder.Ended(), 1)
	require.Equal(t, uint64(1), requireSingleDataPoint(t, reader).Count)
}

func TestTraceClickhouseConn_QueryRowTracedOnScan(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, recorder, reader := newRecordingTracedConn(t, inner)

	row := conn.QueryRow(t.Context(), "SELECT count() FROM chat_session_summaries")
	// driver.Row is lazy: nothing completes until the terminal call.
	require.Empty(t, recorder.Ended())

	var count uint64
	require.NoError(t, row.Scan(&count))

	span := requireSingleSpan(t, recorder)
	require.Equal(t, "clickhouse.query_row", span.Name())
	requireSpanAttr(t, span, "gram.clickhouse.table", "chat_session_summaries")
	require.Equal(t, "success", dataPointAttr(requireSingleDataPoint(t, reader), "gram.outcome"))
}

func TestTraceClickhouseConn_QueryRowScanErrorRecordsFailure(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{queryErr: errors.New("scan failed")}
	conn, recorder, reader := newRecordingTracedConn(t, inner)

	row := conn.QueryRow(t.Context(), "SELECT count() FROM t")
	var count uint64
	require.Error(t, row.Scan(&count))

	require.Equal(t, codes.Error, requireSingleSpan(t, recorder).Status().Code)
	require.Equal(t, "failure", dataPointAttr(requireSingleDataPoint(t, reader), "gram.outcome"))
}

// TestTraceClickhouseConn_QueryRowNilErrCheckDoesNotComplete pins the P2
// review finding: driver.Row.Err() reports pre-execution errors without
// consuming the row, so a nil Err() check before Scan must not complete the
// span with a dispatch-only duration.
func TestTraceClickhouseConn_QueryRowNilErrCheckDoesNotComplete(t *testing.T) {
	t.Parallel()

	inner := &fakeClickhouseConn{}
	conn, recorder, reader := newRecordingTracedConn(t, inner)

	row := conn.QueryRow(t.Context(), "SELECT count() FROM t")
	require.NoError(t, row.Err())
	require.Empty(t, recorder.Ended(), "a nil Err() pre-check must not complete the span")
	require.Empty(t, queryDurationDataPoints(t, reader))

	var count uint64
	require.NoError(t, row.Scan(&count))
	require.Len(t, recorder.Ended(), 1)
	require.Equal(t, uint64(1), requireSingleDataPoint(t, reader).Count)
}
