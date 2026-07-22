package telemetry_test

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// TestTraceClickhouseConn_ServerSideSpanLog proves the wrapper's whole job
// end to end against a real ClickHouse: a query issued through
// o11y.TraceClickhouseConn with a caller span in context makes ClickHouse
// record its server-side execution spans in system.opentelemetry_span_log
// under the caller's trace id, parented on the caller's span id — the
// trace-id joinability that incident forensics rely on (INC-417).
//
// system.opentelemetry_span_log is created lazily on the first span-carrying
// query and flushed periodically; the poll below forces flushes while it
// waits.
func TestTraceClickhouseConn_ServerSideSpanLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	traceUUID := uuid.New()
	callerTraceID := trace.TraceID(traceUUID)
	spanUUID := uuid.New()
	var callerSpanID trace.SpanID
	copy(callerSpanID[:], spanUUID[:8])
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    callerTraceID,
		SpanID:     callerSpanID,
		TraceFlags: trace.FlagsSampled,
		TraceState: trace.TraceState{},
		Remote:     false,
	})
	require.True(t, sc.IsValid())

	conn := o11y.TraceClickhouseConn(ti.chConn)
	require.NoError(t, conn.Exec(
		trace.ContextWithSpanContext(ctx, sc),
		"SELECT count() FROM numbers(100)",
	))

	// ClickHouse stores the span log trace id as a UUID and span ids as
	// UInt64 (big-endian of the 8-byte OTel span id).
	wantTraceID := traceUUID.String()
	wantParentSpanID := binary.BigEndian.Uint64(callerSpanID[:])

	type spanLogRow struct {
		OperationName string            `ch:"operation_name"`
		Kind          string            `ch:"kind"`
		SpanID        uint64            `ch:"span_id"`
		ParentSpanID  uint64            `ch:"parent_span_id"`
		DurationUs    int64             `ch:"duration_us"`
		Attributes    map[string]string `ch:"attribute"`
	}

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		// Force the periodic flush; ignore the error — if the test user ever
		// loses SYSTEM privileges the periodic flush still lands within the
		// polling window.
		_ = ti.chConn.Exec(ctx, "SYSTEM FLUSH LOGS")

		var rows []spanLogRow
		err := ti.chConn.Select(ctx, &rows,
			`SELECT operation_name,
			        toString(kind) AS kind,
			        span_id,
			        parent_span_id,
			        finish_time_us - start_time_us AS duration_us,
			        attribute
			 FROM system.opentelemetry_span_log
			 WHERE toString(trace_id) = ?`,
			wantTraceID,
		)
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotEmpty(c, rows, "ClickHouse must record server-side spans for the forwarded trace id") {
			return
		}

		// The root server-side span parents directly under the caller's span,
		// which is what stitches ClickHouse's execution into the request trace.
		rootFound := false
		for _, row := range rows {
			if row.ParentSpanID == wantParentSpanID {
				rootFound = true
				t.Logf("server-side span under caller span: operation=%q kind=%s span_id=%d parent_span_id=%d duration_us=%d attributes=%v",
					row.OperationName, row.Kind, row.SpanID, row.ParentSpanID, row.DurationUs, row.Attributes)
			}
		}
		assert.True(c, rootFound, "a server-side span must parent under the caller span id, got %d spans", len(rows))
	}, 30*time.Second, 500*time.Millisecond)
}
