package mcpfeedback_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	dataexportsrepo "github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
	"github.com/speakeasy-api/gram/server/internal/streams"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// collector is an OTLP endpoint that keeps what it is sent.
type collector struct {
	server *httptest.Server

	mu       sync.Mutex
	received []*collectorlogsv1.ExportLogsServiceRequest
}

func newCollector(t *testing.T) *collector {
	t.Helper()

	c := &collector{server: nil, mu: sync.Mutex{}, received: nil}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		request := new(collectorlogsv1.ExportLogsServiceRequest)
		if proto.Unmarshal(body, request) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.received = append(c.received, request)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(c.server.Close)
	return c
}

func (c *collector) records() []*logsv1.LogRecord {
	c.mu.Lock()
	defer c.mu.Unlock()

	var records []*logsv1.LogRecord
	for _, request := range c.received {
		for _, resourceLogs := range request.GetResourceLogs() {
			for _, scopeLogs := range resourceLogs.GetScopeLogs() {
				records = append(records, scopeLogs.GetLogRecords()...)
			}
		}
	}
	return records
}

// routeTo sends one data source of the caller's project to a collector.
func routeTo(t *testing.T, ctx context.Context, ti *testInstance, dataSource string, c *collector, sensitiveData string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := dataexportsrepo.New(ti.conn)
	destination, err := queries.CreateOtelDestination(ctx, dataexportsrepo.CreateOtelDestinationParams{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Name:             "Collector for " + dataSource,
		EndpointUrl:      c.server.URL,
		HeadersEncrypted: pgtype.Text{String: "", Valid: false},
		SensitiveData:    pgtype.Text{String: sensitiveData, Valid: true},
	})
	require.NoError(t, err)
	_, err = queries.CreateDataExportRoute(ctx, dataexportsrepo.CreateDataExportRouteParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		ProjectID:         *authCtx.ProjectID,
		DataSource:        dataSource,
		Enabled:           true,
		OtelDestinationID: uuid.NullUUID{UUID: destination.ID, Valid: true},
	})
	require.NoError(t, err)
}

// relayQueuedRecords runs the log records a recorder queued through the real
// transform and relay, the way the pipeline's subscribers do.
func relayQueuedRecords(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()

	var mu sync.Mutex
	var normalized []*otelv1.LogRecord
	publisher := gcp.NewMockPublisher[*otelv1.LogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		mu.Lock()
		defer mu.Unlock()
		record, ok := args.Get(1).(*otelv1.LogRecord)
		require.True(t, ok)
		normalized = append(normalized, record)
	}).Return(gcp.NewSuccessPublishResult()).Maybe()

	transform := otelsvc.NewLogTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), publisher, ti.conn, cache.NoopCache)
	for _, queued := range queuedFor(t, ctx, ti, logRecordTopic) {
		inbound := new(otelv1.InboundLogRecord)
		require.NoError(t, proto.Unmarshal(queued.Message, inbound))
		require.NoError(t, transform.Handle(ctx, inbound, gcp.MessageMetadata{}))
	}

	mu.Lock()
	messages := make([]streams.BatchMessage[*otelv1.LogRecord], len(normalized))
	for i, record := range normalized {
		messages[i] = streams.BatchMessage[*otelv1.LogRecord]{Message: record}
	}
	mu.Unlock()
	require.NotEmpty(t, messages)

	// Unsafe so the relay can reach an httptest server on loopback.
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	relay := otelsvc.NewLogRelayHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, testenv.NewEncryptionClient(t), policy)
	require.NoError(t, relay.HandleBatchWithResult(ctx, messages))
}

func exportedAttributes(record *logsv1.LogRecord) map[string]string {
	attrs := make(map[string]string, len(record.GetAttributes()))
	for _, kv := range record.GetAttributes() {
		attrs[kv.GetKey()] = kv.GetValue().GetStringValue()
	}
	return attrs
}

func TestExportPipelineDeliversAReportToTheFeedbackRouteOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedRemoteBackedServer(t, ctx, ti)
	feedback, telemetry, connections := newCollector(t), newCollector(t), newCollector(t)
	routeTo(t, ctx, ti, dataexports.DataSourceMCPFeedback, feedback, "include")
	routeTo(t, ctx, ti, dataexports.DataSourceProductTelemetry, telemetry, "include")
	routeTo(t, ctx, ti, dataexports.DataSourceMCPConnectionLogs, connections, "include")

	receipt, err := newTestRecorder(t, ti, noLimits).Submit(ctx, targetOf(t, ctx, server), sampleCaller(), sampleReport())
	require.NoError(t, err)
	relayQueuedRecords(t, ctx, ti)

	records := feedback.records()
	require.Len(t, records, 1)
	record := records[0]
	require.Equal(t, "speakeasy.mcp_feedback", record.GetEventName())
	require.Equal(t, logsv1.SeverityNumber_SEVERITY_NUMBER_WARN, record.GetSeverityNumber())
	require.Equal(t, sampleReport().Content, record.GetBody().GetStringValue())

	got := exportedAttributes(record)
	require.Equal(t, receipt.ID.String(), got[string(attr.ExtMcpFeedbackIDKey)])
	require.Equal(t, receipt.ID.String(), got[string(attr.LogRecordUIDKey)], "a destination dedupes redeliveries on the report id")
	require.Equal(t, server.ID.String(), got[string(attr.ExtMcpFeedbackServerIDKey)])
	require.Equal(t, "degraded", got[string(attr.ExtMcpFeedbackSeverityKey)])
	require.Equal(t, "search_logs", got[string(attr.ExtMcpFeedbackToolNameKey)])
	require.Equal(t, "Queried a wider range.", got[string(attr.ExtMcpFeedbackWorkaroundKey)])
	require.Equal(t, "Find a failing request.", got[string(attr.ExtMcpFeedbackIntentionKey)])
	require.Equal(t, "priya@acme.example", got[string(attr.UserEmailKey)])
	require.Equal(t, server.ProjectID.String(), got[string(enrich.ProjectIDKey)], "tenancy comes from the pipeline")

	require.Empty(t, telemetry.records(), "feedback is not product telemetry")
	require.Empty(t, connections.records(), "feedback is not a connection log")
}

// A destination that excludes sensitive data still learns that feedback
// arrived, for which server and tool and how severe, without the agent's
// words or the person who sent it.
func TestExportPipelineRedactsFreeTextAndPeopleForADestinationThatExcludesSensitiveData(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedRemoteBackedServer(t, ctx, ti)
	feedback := newCollector(t)
	routeTo(t, ctx, ti, dataexports.DataSourceMCPFeedback, feedback, "exclude")

	receipt, err := newTestRecorder(t, ti, noLimits).Submit(ctx, targetOf(t, ctx, server), sampleCaller(), sampleReport())
	require.NoError(t, err)
	relayQueuedRecords(t, ctx, ti)

	records := feedback.records()
	require.Len(t, records, 1)
	require.Equal(t, "[REDACTED]", records[0].GetBody().GetStringValue())

	got := exportedAttributes(records[0])
	for _, key := range []attribute.Key{attr.ExtMcpFeedbackWorkaroundKey, attr.ExtMcpFeedbackIntentionKey, attr.ExtMcpFeedbackUserIDKey, attr.UserEmailKey, attr.UserIDKey} {
		require.Equal(t, "[REDACTED]", got[string(key)], key)
	}
	require.Equal(t, receipt.ID.String(), got[string(attr.ExtMcpFeedbackIDKey)])
	require.Equal(t, server.ID.String(), got[string(attr.ExtMcpFeedbackServerIDKey)])
	require.Equal(t, "degraded", got[string(attr.ExtMcpFeedbackSeverityKey)])
	require.Equal(t, "search_logs", got[string(attr.ExtMcpFeedbackToolNameKey)])
	require.Equal(t, "Claude in Claude Code", got[string(attr.ExtMcpFeedbackModelKey)])
	require.Equal(t, "claude-code", got[string(attr.ExtMcpFeedbackClientNameKey)])
}

// A project that never set up a feedback export sends feedback nowhere.
func TestExportPipelineSendsNothingWithoutAFeedbackRoute(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedRemoteBackedServer(t, ctx, ti)
	telemetry := newCollector(t)
	routeTo(t, ctx, ti, dataexports.DataSourceProductTelemetry, telemetry, "include")

	_, err := newTestRecorder(t, ti, noLimits).Submit(ctx, targetOf(t, ctx, server), sampleCaller(), sampleReport())
	require.NoError(t, err)
	relayQueuedRecords(t, ctx, ti)

	require.Empty(t, telemetry.records())
}

// The keys that carry the agent's words and the sender must be on the list a
// destination's sensitive-data setting redacts; the rest must not be.
func TestExportedKeysMatchTheSensitiveDataList(t *testing.T) {
	t.Parallel()

	for _, key := range []attribute.Key{attr.ExtMcpFeedbackWorkaroundKey, attr.ExtMcpFeedbackIntentionKey, attr.ExtMcpFeedbackUserIDKey, attr.UserEmailKey, attr.UserIDKey} {
		require.True(t, dialect.IsSensitiveDataKey(string(key)), key)
	}
	for _, key := range []attribute.Key{
		attr.ExtMcpFeedbackIDKey, attr.ExtMcpFeedbackServerIDKey, attr.ExtMcpFeedbackSeverityKey, attr.ExtMcpFeedbackToolNameKey,
		attr.ExtMcpFeedbackModelKey, attr.ExtMcpFeedbackClientNameKey, attr.ExtMcpFeedbackClientVersionKey, attr.McpSessionIDKey, attr.LogRecordUIDKey,
	} {
		require.False(t, dialect.IsSensitiveDataKey(string(key)), key)
		require.True(t, key == attr.McpSessionIDKey || key == attr.LogRecordUIDKey || enrich.IsMcpFeedbackKey(string(key)), key)
	}
}

// The feedback recorder may write its own keys and nothing else the pipeline reserves.
func TestFeedbackSourceMayWriteOnlyItsOwnReservedKeys(t *testing.T) {
	t.Parallel()

	require.False(t, enrich.IsReservedLogKey(dialect.FeedbackLogSource, string(attr.ExtMcpFeedbackSeverityKey)))
	require.True(t, enrich.IsReservedLogKey(dialect.FeedbackLogSource, string(enrich.ProjectIDKey)), "the recorder may not claim a tenant")
	require.True(t, enrich.IsReservedLogKey(dialect.FeedbackLogSource, string(attr.ExtMcpConnectionOutcomeKey)), "nor write another recorder's keys")
	require.True(t, enrich.IsReservedLogKey(dialect.ConnectionLogSource, string(attr.ExtMcpFeedbackSeverityKey)))
	require.True(t, enrich.IsReservedLogKey("speakeasy", string(attr.ExtMcpFeedbackSeverityKey)), "an ingest edge cannot forge feedback")
}
