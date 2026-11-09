package mcpconnectionattempts_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/resource"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	dataexportsrepo "github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
	"github.com/speakeasy-api/gram/server/internal/otelpub"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/streams"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// pipelineSink is the production adapter's shape, which lives in cmd/gram.
type pipelineSink struct{ logs *otelpub.Logger }

func (pipelineSink) Enabled(context.Context, string) (bool, error) { return true, nil }

func (s pipelineSink) Publish(ctx context.Context, organizationID, projectID, recordID string, record log.Record) error {
	if err := s.logs.Log(otelpub.WithRecordID(otelpub.WithTenant(ctx, organizationID, projectID), recordID), record); err != nil {
		return fmt.Errorf("log connection attempt: %w", err)
	}
	return nil
}

func (s pipelineSink) Close() { s.logs.Close() }

func capturePublisher[M any](t *testing.T) (*gcp.MockPublisher[M], func() []M) {
	t.Helper()

	var mu sync.Mutex
	var published []M
	publisher := gcp.NewMockPublisher[M]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		mu.Lock()
		defer mu.Unlock()
		message, ok := args.Get(1).(M)
		require.True(t, ok)
		published = append(published, message)
	}).Return(gcp.NewSuccessPublishResult()).Maybe()
	return publisher, func() []M {
		mu.Lock()
		defer mu.Unlock()
		return append([]M(nil), published...)
	}
}

func logsAlwaysOn(context.Context, string) (bool, error) { return true, nil }

func exportedRecords(request *collectorlogsv1.ExportLogsServiceRequest) []*logsv1.LogRecord {
	var records []*logsv1.LogRecord
	for _, resourceLogs := range request.GetResourceLogs() {
		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			records = append(records, scopeLogs.GetLogRecords()...)
		}
	}
	return records
}

func attributesOf(record *logsv1.LogRecord) map[string]*commonv1.AnyValue {
	values := make(map[string]*commonv1.AnyValue, len(record.GetAttributes()))
	for _, kv := range record.GetAttributes() {
		values[kv.GetKey()] = kv.GetValue()
	}
	return values
}

func TestExportPipeline_RealRecordReachesASensitiveExcludedDestinationUnredacted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	infra := mcpconnectionattempts.PipelineInfra()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	var mu sync.Mutex
	var received []*collectorlogsv1.ExportLogsServiceRequest
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		request := new(collectorlogsv1.ExportLogsServiceRequest)
		if proto.Unmarshal(body, request) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		received = append(received, request)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)

	organizationID := "org-pipeline"
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Pipeline", Slug: "pipeline-" + uuid.NewString()[:8], OrganizationID: organizationID})
	require.NoError(t, err)
	queries := dataexportsrepo.New(db)
	destination, err := queries.CreateOtelDestination(ctx, dataexportsrepo.CreateOtelDestinationParams{
		OrganizationID:   organizationID,
		ProjectID:        project.ID,
		Name:             "Collector",
		EndpointUrl:      collector.URL,
		HeadersEncrypted: pgtype.Text{},
		SensitiveData:    pgtype.Text{String: "exclude", Valid: true},
	})
	require.NoError(t, err)
	_, err = queries.CreateDataExportRoute(ctx, dataexportsrepo.CreateDataExportRouteParams{
		OrganizationID:    organizationID,
		ProjectID:         project.ID,
		DataSource:        dataexports.DataSourceMCPConnectionLogs,
		Enabled:           true,
		OtelDestinationID: uuid.NullUUID{UUID: destination.ID, Valid: true},
	})
	require.NoError(t, err)

	inboundPublisher, inbound := capturePublisher[*otelv1.InboundLogRecord](t)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	recorder := mcpconnectionattempts.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, logsAlwaysOn,
		mcpconnectionattempts.WithExport(pipelineSink{logs: otelpub.NewLogger(inboundPublisher, resource.Empty(), dialect.ConnectionLogScope)}))
	recorder.End(ctx, mcpconnectionattempts.Begin(mcpconnectionattempts.ServerScope(organizationID, project.ID, uuid.New(), mcpconnectionattempts.AppDCR)),
		mcpconnectionattempts.StepProviderToken, mcpconnectionattempts.CodeProviderUnavailable)
	require.NoError(t, recorder.Shutdown(ctx))
	require.Len(t, inbound(), 1, "otelpub accepted the record")

	normalizedPublisher, normalized := capturePublisher[*otelv1.LogRecord](t)
	transform := otelsvc.NewLogTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), normalizedPublisher, db, cache.NoopCache)
	require.NoError(t, transform.Handle(ctx, inbound()[0], gcp.MessageMetadata{}))
	require.Len(t, normalized(), 1)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	relay := otelsvc.NewLogRelayHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), db, testenv.NewEncryptionClient(t), policy)
	require.NoError(t, relay.HandleBatchWithResult(ctx, []streams.BatchMessage[*otelv1.LogRecord]{{Message: normalized()[0]}}))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 1)
	records := exportedRecords(received[0])
	require.Len(t, records, 1)
	require.Nil(t, records[0].GetBody(), "no body means nothing to redact")
	require.Equal(t, "speakeasy.mcp_connection", records[0].GetEventName())

	got := attributesOf(records[0])
	require.Equal(t, "failed", got[string(attr.ExtMcpConnectionOutcomeKey)].GetStringValue())
	require.Equal(t, mcpconnectionattempts.CodeProviderUnavailable.String(), got[string(attr.ErrorTypeKey)].GetStringValue())
	require.True(t, got[string(attr.ExtMcpConnectionFinalKey)].GetBoolValue())
	require.NotEmpty(t, got[string(attr.ExtMcpConnectionUserMessageKey)].GetStringValue())
	require.NotEmpty(t, got[string(attr.ExtMcpConnectionUserNextStepKey)].GetStringValue())
	require.Equal(t, project.ID.String(), got[string(enrich.ProjectIDKey)].GetStringValue(), "tenancy still comes from the pipeline")
	for key, value := range got {
		require.NotEqual(t, "[REDACTED]", value.GetStringValue(), key)
	}
}
