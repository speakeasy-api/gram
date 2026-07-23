package telemetry_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// recordCapture collects published LogRecords from a MockPublisher's Run hook.
// The drain goroutine only touches PublishResults, but a mutex keeps the
// capture race-clean regardless.
type recordCapture struct {
	mu      sync.Mutex
	records []*telemetryv1.LogRecord
}

func (c *recordCapture) add(rec *telemetryv1.LogRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, rec)
}

func (c *recordCapture) all() []*telemetryv1.LogRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*telemetryv1.LogRecord, len(c.records))
	copy(out, c.records)
	return out
}

func newCapturingMockPublisher(result any) (*gcp.MockPublisher[*telemetryv1.LogRecord], *recordCapture) {
	capture := &recordCapture{mu: sync.Mutex{}, records: nil}
	mockPub := gcp.NewMockPublisher[*telemetryv1.LogRecord]()
	mockPub.On("Publish", mock.Anything, mock.Anything).Return(result).Run(func(args mock.Arguments) {
		rec, ok := args.Get(1).(*telemetryv1.LogRecord)
		if ok {
			capture.add(rec)
		}
	})
	return mockPub, capture
}

func shadowFlagsOn() *feature.InMemory {
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagTelemetryLogsPubSubShadow, telemetry.ShadowFlagDistinctID, true)
	return flags
}

// newShadowTestLogger builds a Logger backed by the shared test ClickHouse
// whose shadow publisher uses the given publisher and flags. The LogPublisher
// is returned alongside so tests can await its ack drains.
func newShadowTestLogger(t *testing.T, ctx context.Context, ti *testInstance, pub gcp.Publisher[*telemetryv1.LogRecord], flags feature.Provider) (*telemetry.Logger, *telemetry.LogPublisher) {
	t.Helper()

	logger := testenv.NewLogger(t)
	enabled := func(context.Context, string) (bool, error) { return true, nil }
	logPub := telemetry.NewLogPublisher(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), pub, flags)
	return telemetry.NewLogger(ctx, logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.chConn, enabled, enabled, nil, logPub), logPub
}

// fetchLog reads back exactly one telemetry_logs row for the given tool.
// Callers must flush the async insert queue first
// (testenv.FlushClickHouseAsyncInserts) so the single query is deterministic.
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

// TestLogPublisher_MirrorsRowsToPubSub verifies the core invariant of the
// shadow dual-write: every row that lands in telemetry_logs is published to
// Pub/Sub with the same id and content.
func TestLogPublisher_MirrorsRowsToPubSub(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	mockPub, capture := newCapturingMockPublisher(gcp.NewSuccessPublishResult())
	telemLogger, logPub := newShadowTestLogger(t, ctx, ti, mockPub, shadowFlagsOn())

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

	recordsByID := make(map[string]*telemetryv1.LogRecord, len(records))
	for _, rec := range records {
		recordsByID[rec.GetId()] = rec
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
		require.Equal(t, chLog.projectID, rec.GetGramProjectId())
		require.Equal(t, chLog.urn, rec.GetGramUrn())
		require.Equal(t, timestamp.UnixNano(), rec.GetTimeUnixNano())
		require.Contains(t, rec.GetAttributesJson(), chLog.urn)
		require.NotEmpty(t, rec.GetResourceAttributesJson())
		require.Equal(t, "gram-server", rec.GetServiceName())
		// Rows without trace context keep the nullable columns unset.
		require.False(t, rec.HasTraceId())
		require.False(t, rec.HasSpanId())
		require.False(t, rec.HasGramChatId())
		require.True(t, rec.HasSeverityText(), "severity defaults to INFO through the Logger")
	}

	// Await the batch's ack drain so no goroutine outlives the test.
	logPub.WaitForPublishDrains()
}

// TestLogPublisher_FlagOffPublishesNothing verifies the killswitch: with the
// shadow flag off (the fail-closed default), rows land in ClickHouse and
// nothing is published.
func TestLogPublisher_FlagOffPublishesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	mockPub := gcp.NewMockPublisher[*telemetryv1.LogRecord]()
	telemLogger, _ := newShadowTestLogger(t, ctx, ti, mockPub, &feature.InMemory{})

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("GET")
	attrs.RecordStatusCode(200)

	toolInfo := newTestToolInfo(ti.orgID)
	timestamp := time.Now().UTC()
	require.NoError(t, telemLogger.LogBulk(ctx, []telemetry.LogParams{
		{Timestamp: timestamp, ToolInfo: toolInfo, Attributes: attrs},
	}))

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	fetchLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)
	mockPub.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
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
		shadowFlagsOn(),
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
		Attributes:           "{}",
		ResourceAttributes:   "{}",
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
	require.Equal(t, "log-id-1", records[0].GetId())
}

// TestLogPublisher_PublishFailureDoesNotAffectWrite verifies the best-effort
// contract: a failing publish ack leaves the ClickHouse write untouched.
func TestLogPublisher_PublishFailureDoesNotAffectWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	mockPub, capture := newCapturingMockPublisher(errors.New("broker unavailable"))
	telemLogger, logPub := newShadowTestLogger(t, ctx, ti, mockPub, shadowFlagsOn())

	attrs := telemetry.HTTPLogAttributes{}
	attrs.RecordMethod("GET")
	attrs.RecordStatusCode(500)

	toolInfo := newTestToolInfo(ti.orgID)
	timestamp := time.Now().UTC()
	require.NoError(t, telemLogger.LogBulk(ctx, []telemetry.LogParams{
		{Timestamp: timestamp, ToolInfo: toolInfo, Attributes: attrs},
	}))

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	fetchLog(t, ctx, ti.chClient, toolInfo.ProjectID, toolInfo.URN, timestamp)
	require.Len(t, capture.all(), 1)

	// Await the drain so the failure path (error log) fully executes within
	// the test's lifetime.
	logPub.WaitForPublishDrains()
}
