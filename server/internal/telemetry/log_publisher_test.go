package telemetry_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// recordCapture collects published LogRecords from a MockPublisher's Run hook.
// The drain goroutine only touches PublishResults, but a mutex keeps the
// capture race-clean regardless.
type recordCapture struct {
	mu      sync.Mutex
	records []*otelv1.InboundLogRecord
}

func (c *recordCapture) add(rec *otelv1.InboundLogRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, rec)
}

func (c *recordCapture) all() []*otelv1.InboundLogRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*otelv1.InboundLogRecord, len(c.records))
	copy(out, c.records)
	return out
}

func newCapturingMockPublisher(result any) (*gcp.MockPublisher[*otelv1.InboundLogRecord], *recordCapture) {
	capture := &recordCapture{mu: sync.Mutex{}, records: nil}
	mockPub := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	mockPub.On("Publish", mock.Anything, mock.Anything).Return(result).Run(func(args mock.Arguments) {
		rec, ok := args.Get(1).(*otelv1.InboundLogRecord)
		if ok {
			capture.add(rec)
		}
	})
	return mockPub, capture
}

// newShadowTestLogger builds a Logger backed by the shared test ClickHouse
// whose shadow publisher uses the given publisher. The LogPublisher is
// returned alongside so tests can await its ack drains.
func newShadowTestLogger(t *testing.T, ctx context.Context, ti *testInstance, pub gcp.Publisher[*otelv1.InboundLogRecord]) (*telemetry.Logger, *telemetry.LogPublisher) {
	t.Helper()

	logger := testenv.NewLogger(t)
	enabled := func(context.Context, string) (bool, error) { return true, nil }
	logPub := telemetry.NewLogPublisher(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), pub)
	return telemetry.NewLogger(ctx, logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.chConn, enabled, enabled, nil, logPub), logPub
}

// fetchLog reads back exactly one telemetry_logs row for the given tool.
// Callers using an async write path must flush the insert queue first with
// testenv.FlushClickHouseAsyncInserts.
func fetchLog(t *testing.T, ctx context.Context, client *repo.Queries, projectID, urn string, timestamp time.Time) repo.TelemetryLog {
	t.Helper()

	logs, err := client.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID: projectID,
		TimeStart:     timestamp.Add(-1 * time.Minute).UnixNano(),
		TimeEnd:       timestamp.Add(1 * time.Minute).UnixNano(),
		GramURNs:      []string{urn},
		SortOrder:     "desc",
		Cursor:        "",
		Limit:         10,
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	return logs[0]
}

// TestLogPublisher_PublishesCanonicalOTELRows verifies the ingestion seam:
// every internal telemetry row is converted to the canonical inbound OTEL
// shape with stable identity, authenticated tenancy, and typed attributes.
func TestLogPublisher_PublishesCanonicalOTELRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	mockPub, capture := newCapturingMockPublisher(gcp.NewSuccessPublishResult())
	telemLogger, logPub := newShadowTestLogger(t, ctx, ti, mockPub)

	toolInfoA := newTestToolInfo(ti.orgID)
	toolInfoB := newTestToolInfo(ti.orgID)
	timestamp := time.Now().UTC()

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("POST")
	attrs.RecordStatusCode(200)

	require.NoError(t, telemLogger.LogBulk(ctx, []telemetry.LogParams{
		{Timestamp: timestamp, ToolInfo: toolInfoA, Attributes: attrs},
		{Timestamp: timestamp, ToolInfo: toolInfoB, Attributes: attrs},
	}))

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	logA := fetchLog(t, ctx, ti.chClient, toolInfoA.ProjectID, toolInfoA.URN, timestamp)
	logB := fetchLog(t, ctx, ti.chClient, toolInfoB.ProjectID, toolInfoB.URN, timestamp)

	// Publishes happen synchronously inside LogBulk; only the ack drain is
	// async, so the capture is already complete here.
	records := capture.all()
	require.Len(t, records, 2)

	recordsByID := make(map[string]*otelv1.InboundLogRecord, len(records))
	for _, rec := range records {
		recordsByID[rec.GetRecordId()] = rec
	}
	for _, chLog := range []struct {
		id        string
		projectID string
		urn       string
	}{
		{id: logA.ID, projectID: toolInfoA.ProjectID, urn: toolInfoA.URN},
		{id: logB.ID, projectID: toolInfoB.ProjectID, urn: toolInfoB.URN},
	} {
		rec, ok := recordsByID[chLog.id]
		require.True(t, ok, "published records must carry the ClickHouse row id")
		require.Equal(t, chLog.projectID, rec.GetProvenance().GetProjectId())
		require.Equal(t, ti.orgID, rec.GetProvenance().GetOrganizationId())
		require.Equal(t, uint64(timestamp.UnixNano()), rec.GetTimeUnixNano())
		require.Equal(t, chLog.urn, inboundStringAttribute(rec.GetAttributes(), string(attr.ToolURNKey)))
		require.Equal(t, ti.orgID, inboundStringAttribute(rec.GetAttributes(), string(attr.OrganizationIDKey)))
		require.Equal(t, "gram-server", inboundStringAttribute(rec.GetResource().GetAttributes(), string(attr.ServiceNameKey)))
		// Rows without trace context keep the canonical byte fields empty.
		require.Empty(t, rec.GetTraceId())
		require.Empty(t, rec.GetSpanId())
		require.Empty(t, inboundStringAttribute(rec.GetAttributes(), string(attr.GenAIConversationIDKey)))
		require.True(t, rec.HasSeverityText(), "severity defaults to INFO through the Logger")
		require.Equal(t, otelv1.InboundLogRecord_SEVERITY_NUMBER_INFO, rec.GetSeverityNumber())
	}

	// Await the batch's ack drain so no goroutine outlives the test.
	logPub.WaitForPublishDrains()
}

// TestLogPublisher_PublishesDespiteCanceledContext verifies that caller
// cancellation does not drop the shadow copy. PublishLogs runs after ClickHouse
// accepted the rows, and a row skipped at that point is never re-published: a
// retry finds it already in telemetry_logs and takes the dedupe path, leaving a
// permanent gap in the mirror.
func TestLogPublisher_PublishesDespiteCanceledContext(t *testing.T) {
	t.Parallel()

	mockPub, capture := newCapturingMockPublisher(gcp.NewSuccessPublishResult())
	logPub := telemetry.NewLogPublisher(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		mockPub,
	)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	logPub.PublishLogs(ctx, []repo.InsertTelemetryLogParams{{
		ID:                   "log-id-1",
		TimeUnixNano:         time.Now().UnixNano(),
		ObservedTimeUnixNano: time.Now().UnixNano(),
		SeverityText:         nil,
		Body:                 "",
		TraceID:              nil,
		SpanID:               nil,
		Attributes:           `{"gram.org.id":"org-1"}`,
		ResourceAttributes:   `{"service.name":"gram-server"}`,
		GramProjectID:        "project-1",
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "tools:http:test:tool",
		ServiceName:          "gram-server",
		ServiceVersion:       nil,
		GramChatID:           nil,
	}})
	logPub.WaitForPublishDrains()

	records := capture.all()
	require.Len(t, records, 1)
	require.Equal(t, "log-id-1", records[0].GetRecordId())
}

// TestLogPublisher_PublishFailureDoesNotAffectWrite verifies the best-effort
// contract: a failing publish ack leaves the ClickHouse write untouched.
func TestLogPublisher_PublishFailureDoesNotAffectWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	mockPub, capture := newCapturingMockPublisher(errors.New("broker unavailable"))
	telemLogger, logPub := newShadowTestLogger(t, ctx, ti, mockPub)

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("GET")
	attrs.RecordStatusCode(500)

	toolInfo := newTestToolInfo(ti.orgID)
	timestamp := time.Now().UTC()
	// The deduped path uses the same publisher after a synchronous ClickHouse
	// write. With no fingerprint attribute, it writes this row unchanged.
	written, dropped, err := telemLogger.LogBulkDeduped(ctx, attr.CodexComplianceEventHashKey, []telemetry.LogParams{
		{Timestamp: timestamp, ToolInfo: toolInfo, Attributes: attrs},
	})
	require.NoError(t, err)
	require.Equal(t, 1, written)
	require.Equal(t, 0, dropped)

	fetchLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)
	require.Len(t, capture.all(), 1)

	// Await the drain so the failure path (error log) fully executes within
	// the test's lifetime.
	logPub.WaitForPublishDrains()
}

func inboundStringAttribute(attributes []*otelv1.InboundLogRecord_KeyValue, key string) string {
	for _, attribute := range attributes {
		if attribute.GetKey() == key {
			return attribute.GetValue().GetStringValue()
		}
	}
	return ""
}
