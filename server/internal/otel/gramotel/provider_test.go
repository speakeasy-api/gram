package gramotel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	testOrganizationID = "org-1"
	testLoggerName     = "github.com/speakeasy-api/gram/server/internal/mcp"
)

var testProjectID = uuid.MustParse("01900000-0000-7000-8000-000000000101")

func authenticated(ctx context.Context) context.Context {
	projectID := testProjectID
	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  testOrganizationID,
		UserID:                "",
		ExternalUserID:        "",
		APIKeyID:              "",
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		SessionID:             nil,
		ProjectID:             &projectID,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})
}

// capture records every published log record on a mock publisher, so a test
// can assert what reached the topic.
func capture(t *testing.T, result gcp.PublishResult) (*gcp.MockPublisher[*otelv1.InboundLogRecord], *[]*otelv1.InboundLogRecord) {
	t.Helper()
	var published []*otelv1.InboundLogRecord
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.InboundLogRecord)
		require.True(t, ok)
		published = append(published, record)
	}).Return(result).Maybe()
	return publisher, &published
}

func testLogger(t *testing.T, publisher gcp.Publisher[*otelv1.InboundLogRecord]) log.Logger {
	t.Helper()
	res := resource.NewSchemaless(semconv.ServiceName("gram-server"))
	provider := NewLoggerProvider(testenv.NewLogger(t), nil, publisher, res)
	return provider.Logger(testLoggerName)
}

func toolCallRecord(at time.Time, attrs ...log.KeyValue) log.Record {
	var record log.Record
	record.SetEventName("gram.tool_call.started")
	record.SetTimestamp(at)
	record.SetSeverity(log.SeverityInfo)
	record.SetBody(log.StringValue("tool call started"))
	record.AddAttributes(log.String("gram.tool_call.id", "call-1"))
	record.AddAttributes(attrs...)
	return record
}

func attributeValue(record *otelv1.InboundLogRecord, key string) string {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func TestEmitPublishesBeforeReturningWithTenancyFromTheAuthenticatedRequest(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger := testLogger(t, publisher)
	ctx, result := WithResult(authenticated(t.Context()))

	logger.Emit(ctx, toolCallRecord(time.Unix(1_700_000_000, 0)))

	// Emit is synchronous: the record is on the topic by the time it returns.
	require.Len(t, *published, 1)
	require.NoError(t, result.Err())

	record := (*published)[0]
	require.Equal(t, testOrganizationID, record.GetProvenance().GetOrganizationId())
	require.Equal(t, testProjectID.String(), record.GetProvenance().GetProjectId())
	require.Equal(t, ProvenanceSource, record.GetProvenance().GetSource())
	require.Equal(t, "gram.tool_call.started", record.GetEventName())
	require.Equal(t, "call-1", attributeValue(record, "gram.tool_call.id"))
	require.Equal(t, "tool call started", record.GetBody().GetStringValue())
	require.Equal(t, otelv1.InboundLogRecord_SEVERITY_NUMBER_INFO, record.GetSeverityNumber())
	require.Equal(t, testLoggerName, record.GetScope().GetName())
	require.Equal(t, "gram-server", resourceServiceName(record))
	require.NotZero(t, record.GetObservedTimeUnixNano(), "observed time is stamped")
	require.NotEmpty(t, record.GetRecordId())
}

func resourceServiceName(record *otelv1.InboundLogRecord) string {
	for _, kv := range record.GetResource().GetAttributes() {
		if kv.GetKey() == "service.name" {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func TestEmitTakesTenancyFromWithTenantOutsideARequest(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger := testLogger(t, publisher)
	ctx, result := WithResult(WithTenant(t.Context(), "org-2", "project-2"))

	logger.Emit(ctx, toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.NoError(t, result.Err())
	require.Len(t, *published, 1)
	require.Equal(t, "org-2", (*published)[0].GetProvenance().GetOrganizationId())
	require.Equal(t, "project-2", (*published)[0].GetProvenance().GetProjectId())
}

func TestEmitRefusesARecordWithNoTenancy(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger := testLogger(t, publisher)
	ctx, result := WithResult(t.Context())

	logger.Emit(ctx, toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.ErrorIs(t, result.Err(), ErrInvalid)
	require.Empty(t, *published)
}

func TestEmitUsesTheRecordIDTheCallerSets(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger := testLogger(t, publisher)
	ctx, result := WithResult(WithRecordID(authenticated(t.Context()), "call-1"))

	logger.Emit(ctx, toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.NoError(t, result.Err())
	require.Equal(t, "call-1", (*published)[0].GetRecordId())
}

func TestEmitDerivesTheSameRecordIDForTheSameRecord(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger := testLogger(t, publisher)
	ctx := authenticated(t.Context())
	at := time.Unix(1_700_000_000, 0)

	logger.Emit(ctx, toolCallRecord(at))
	logger.Emit(ctx, toolCallRecord(at))
	logger.Emit(ctx, toolCallRecord(at.Add(time.Second)))

	require.Len(t, *published, 3)
	require.Equal(t, (*published)[0].GetRecordId(), (*published)[1].GetRecordId(), "the same record collapses to one id, whatever its observed time")
	require.NotEqual(t, (*published)[0].GetRecordId(), (*published)[2].GetRecordId())
}

func TestEmitRefusesTheReservedNamespace(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger := testLogger(t, publisher)
	ctx, result := WithResult(authenticated(t.Context()))

	logger.Emit(ctx, toolCallRecord(time.Unix(1_700_000_000, 0), log.String(string(enrich.EventTypeColumnKey), "tool_call")))

	require.ErrorIs(t, result.Err(), ErrInvalid)
	require.ErrorContains(t, result.Err(), "reserved namespace")
	require.Empty(t, *published)
}

func TestEmitRefusesARecordThatBreaksTheIngestContract(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger := testLogger(t, publisher)
	ctx, result := WithResult(authenticated(t.Context()))

	logger.Emit(ctx, toolCallRecord(time.Unix(1_700_000_000, 0), log.String("payload", strings.Repeat("x", MaxLogRecordBytes))))

	require.ErrorIs(t, result.Err(), ErrInvalid)
	require.Empty(t, *published)
}

func TestEmitReportsAPublishFailureThroughTheResult(t *testing.T) {
	t.Parallel()

	publisher, _ := capture(t, gcp.NewErrPublishResult(errors.New("pubsub unavailable")))
	logger := testLogger(t, publisher)
	ctx, result := WithResult(authenticated(t.Context()))

	logger.Emit(ctx, toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.ErrorContains(t, result.Err(), "pubsub unavailable")
	require.NotErrorIs(t, result.Err(), ErrInvalid)
}

func TestEmitWithoutAResultStillPublishes(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger := testLogger(t, publisher)

	logger.Emit(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.Len(t, *published, 1)
}
