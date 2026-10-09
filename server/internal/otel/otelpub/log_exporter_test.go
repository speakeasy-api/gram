package otelpub

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
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

func testLogger(t *testing.T, publisher gcp.Publisher[*otelv1.InboundLogRecord]) (log.Logger, *bytes.Buffer) {
	t.Helper()
	return testLoggerWithResource(t, publisher, resource.NewSchemaless(semconv.ServiceName("gram-server")))
}

// testLoggerWithResource also returns what otelpub logged, where refusals and publish failures surface.
func testLoggerWithResource(t *testing.T, publisher gcp.Publisher[*otelv1.InboundLogRecord], res *resource.Resource) (log.Logger, *bytes.Buffer) {
	t.Helper()
	var warnings bytes.Buffer
	provider := NewLoggerProvider(slog.New(slog.NewTextHandler(&warnings, nil)), publisher, res)
	return provider.Logger(testLoggerName), &warnings
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
	logger, warnings := testLogger(t, publisher)

	logger.Emit(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0)))

	// Emit is synchronous: the record is on the topic by the time it returns.
	require.Len(t, *published, 1)
	require.Empty(t, warnings.String())

	record := (*published)[0]
	require.Equal(t, testOrganizationID, record.GetProvenance().GetOrganizationId())
	require.Equal(t, testProjectID.String(), record.GetProvenance().GetProjectId())
	require.Equal(t, otelsvc.ProvenanceSource, record.GetProvenance().GetSource())
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
	logger, _ := testLogger(t, publisher)

	logger.Emit(WithTenant(t.Context(), "org-2", "project-2"), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.Len(t, *published, 1)
	require.Equal(t, "org-2", (*published)[0].GetProvenance().GetOrganizationId())
	require.Equal(t, "project-2", (*published)[0].GetProvenance().GetProjectId())
}

func TestEmitRefusesARecordWithNoTenancy(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger, warnings := testLogger(t, publisher)

	logger.Emit(t.Context(), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.Empty(t, *published)
	require.Contains(t, warnings.String(), "no tenancy in context")
	require.Contains(t, warnings.String(), "gram.tool_call.started")
}

func TestEmitRefusesAnIncompleteTenantInsteadOfFallingBackToTheRequest(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger, warnings := testLogger(t, publisher)

	logger.Emit(WithTenant(authenticated(t.Context()), "org-2", ""), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.Empty(t, *published)
	require.Contains(t, warnings.String(), "no tenancy in context")
}

func TestEmitUsesTheRecordIDTheCallerSets(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger, _ := testLogger(t, publisher)

	logger.Emit(WithRecordID(authenticated(t.Context()), "call-1"), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.Len(t, *published, 1)
	require.Equal(t, "call-1", (*published)[0].GetRecordId())
}

func TestEmitDerivesTheSameRecordIDForTheSameRecord(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger, _ := testLogger(t, publisher)
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
	logger, warnings := testLogger(t, publisher)

	logger.Emit(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0), log.String(string(enrich.AgentEventTypeKey), "tool_call")))

	require.Empty(t, *published)
	require.Contains(t, warnings.String(), "reserved namespace")
}

func TestEmitRefusesTheDirectoryNamespace(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger, warnings := testLogger(t, publisher)

	logger.Emit(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0), log.String("directory.department", "engineering")))

	require.Empty(t, *published)
	require.Contains(t, warnings.String(), "reserved namespace")
}

func TestEmitRefusesTheReservedNamespaceOnTheResource(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	res := resource.NewSchemaless(attribute.String(string(enrich.AgentEventTypeKey), "tool_call"))
	logger, warnings := testLoggerWithResource(t, publisher, res)

	logger.Emit(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.Empty(t, *published)
	require.Contains(t, warnings.String(), "reserved namespace")
}

func TestEmitRefusesARecordThatBreaksTheIngestContract(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger, warnings := testLogger(t, publisher)

	logger.Emit(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0), log.String("payload", strings.Repeat("x", 4*constants.MiB))))

	require.Empty(t, *published)
	require.Contains(t, warnings.String(), "exceeds maximum size")
}

func TestEmitLogsAPublishFailureWithoutPanicking(t *testing.T) {
	t.Parallel()

	publisher, _ := capture(t, gcp.NewErrPublishResult(errors.New("pubsub unavailable")))
	logger, warnings := testLogger(t, publisher)

	require.NotPanics(t, func() {
		logger.Emit(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0)))
	})

	require.Contains(t, warnings.String(), "pubsub unavailable")
	require.Contains(t, warnings.String(), "gram.tool_call.started")
}

func TestEmitKeepsEmptyAndNilBytesValues(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger, warnings := testLogger(t, publisher)

	logger.Emit(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0),
		log.Slice("list", log.StringValue("a"), log.Value{}),
		log.Bytes("raw", nil),
	))

	require.Len(t, *published, 1)
	require.Empty(t, warnings.String())
}

func TestUnixNanoSaturatesInsteadOfOverflowing(t *testing.T) {
	t.Parallel()

	require.Zero(t, unixNano(time.Time{}))
	require.Zero(t, unixNano(time.Unix(-1, 0)))
	require.Equal(t, uint64(1_700_000_000_000_000_001), unixNano(time.Unix(1_700_000_000, 1)))
	require.Equal(t, uint64(math.MaxUint64), unixNano(time.Unix(1<<62, 0)))
}

// barrierPublisher holds every Publish until want of them are in flight at once.
type barrierPublisher struct {
	arrived chan struct{}
	release chan struct{}
}

func (p *barrierPublisher) Publish(context.Context, *otelv1.InboundLogRecord, ...gcp.PublishOption) gcp.PublishResult {
	p.arrived <- struct{}{}
	<-p.release
	return gcp.NewSuccessPublishResult()
}

func (*barrierPublisher) Stop(context.Context) error { return nil }

func TestConcurrentEmitsPublishInParallel(t *testing.T) {
	t.Parallel()

	const emits = 2
	publisher := &barrierPublisher{arrived: make(chan struct{}, emits), release: make(chan struct{})}
	logger, _ := testLogger(t, publisher)
	ctx := authenticated(t.Context())

	done := make(chan struct{}, emits)
	for range emits {
		go func() {
			logger.Emit(ctx, toolCallRecord(time.Now()))
			done <- struct{}{}
		}()
	}
	defer close(publisher.release)
	for range emits {
		select {
		case <-publisher.arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("emits were serialized: a second publish never started while the first was in flight")
		}
	}
}

func TestShutdownWaitsForInFlightEmitsAndStopsNewOnes(t *testing.T) {
	t.Parallel()

	publisher := &barrierPublisher{arrived: make(chan struct{}, 2), release: make(chan struct{})}
	provider := NewLoggerProvider(testenv.NewLogger(t), publisher, resource.Empty())
	logger := provider.Logger(testLoggerName)
	ctx := authenticated(t.Context())

	go logger.Emit(ctx, toolCallRecord(time.Now()))
	<-publisher.arrived

	shutdown := make(chan error, 1)
	go func() { shutdown <- provider.Shutdown(t.Context()) }()
	select {
	case <-shutdown:
		t.Fatal("shutdown returned while an emit was still publishing")
	case <-time.After(100 * time.Millisecond):
	}
	close(publisher.release)
	require.NoError(t, <-shutdown)

	logger.Emit(ctx, toolCallRecord(time.Now()))
	require.Empty(t, publisher.arrived, "an emit after shutdown publishes nothing")
}
