package otelpub

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/stretchr/testify/assert"
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
)

const (
	testOrganizationID = "org-1"
	testScope          = "github.com/speakeasy-api/gram/server/internal/mcp"
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

func testLogger(publisher gcp.Publisher[*otelv1.InboundLogRecord]) *Logger {
	return NewLogger(publisher, resource.NewSchemaless(semconv.ServiceName("gram-server")), testScope)
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

func resourceServiceName(record *otelv1.InboundLogRecord) string {
	for _, kv := range record.GetResource().GetAttributes() {
		if kv.GetKey() == "service.name" {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func TestLogPublishesBeforeReturningWithTenancyFromTheAuthenticatedRequest(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())

	err := testLogger(publisher).Log(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.NoError(t, err)
	require.Len(t, *published, 1)
	record := (*published)[0]
	require.Equal(t, testOrganizationID, record.GetProvenance().GetOrganizationId())
	require.Equal(t, testProjectID.String(), record.GetProvenance().GetProjectId())
	require.Equal(t, otelsvc.ProvenanceSource, record.GetProvenance().GetSource())
	require.Equal(t, "gram.tool_call.started", record.GetEventName())
	require.Equal(t, "call-1", attributeValue(record, "gram.tool_call.id"))
	require.Equal(t, "tool call started", record.GetBody().GetStringValue())
	require.Equal(t, otelv1.InboundLogRecord_SEVERITY_NUMBER_INFO, record.GetSeverityNumber())
	require.Equal(t, testScope, record.GetScope().GetName())
	require.Equal(t, "gram-server", resourceServiceName(record))
	require.NotZero(t, record.GetObservedTimeUnixNano(), "observed time is stamped")
	require.NotEmpty(t, record.GetRecordId())
}

func TestLogTakesTenancyFromWithTenantOutsideARequest(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())

	err := testLogger(publisher).Log(WithTenant(t.Context(), "org-2", "project-2"), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.NoError(t, err)
	require.Len(t, *published, 1)
	require.Equal(t, "org-2", (*published)[0].GetProvenance().GetOrganizationId())
	require.Equal(t, "project-2", (*published)[0].GetProvenance().GetProjectId())
}

func TestLogRefusesARecordWithNoTenancy(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())

	err := testLogger(publisher).Log(t.Context(), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.ErrorContains(t, err, "no tenancy in context")
	require.Empty(t, *published)
}

func TestLogRefusesAnIncompleteTenantInsteadOfFallingBackToTheRequest(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())

	err := testLogger(publisher).Log(WithTenant(authenticated(t.Context()), "org-2", ""), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.ErrorContains(t, err, "no tenancy in context")
	require.Empty(t, *published)
}

func TestLogUsesTheRecordIDTheCallerSets(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())

	err := testLogger(publisher).Log(WithRecordID(authenticated(t.Context()), "call-1"), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.NoError(t, err)
	require.Len(t, *published, 1)
	require.Equal(t, "call-1", (*published)[0].GetRecordId())
}

func TestLogDerivesTheSameRecordIDForTheSameRecord(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger := testLogger(publisher)
	ctx := authenticated(t.Context())
	at := time.Unix(1_700_000_000, 0)

	require.NoError(t, logger.Log(ctx, toolCallRecord(at)))
	require.NoError(t, logger.Log(ctx, toolCallRecord(at)))
	require.NoError(t, logger.Log(ctx, toolCallRecord(at.Add(time.Second))))

	require.Len(t, *published, 3)
	require.Equal(t, (*published)[0].GetRecordId(), (*published)[1].GetRecordId(), "the same record collapses to one id, whatever its observed time")
	require.NotEqual(t, (*published)[0].GetRecordId(), (*published)[2].GetRecordId())
}

func TestLogRefusesTheReservedNamespace(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())

	err := testLogger(publisher).Log(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0), log.String(string(enrich.AgentEventTypeKey), "tool_call")))

	require.ErrorContains(t, err, "reserved namespace")
	require.Empty(t, *published)
}

func TestLogRefusesTheDirectoryNamespace(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())

	err := testLogger(publisher).Log(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0), log.String("directory.department", "engineering")))

	require.ErrorContains(t, err, "reserved namespace")
	require.Empty(t, *published)
}

func TestLogRefusesTheReservedNamespaceOnTheResource(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger := NewLogger(publisher, resource.NewSchemaless(attribute.String(string(enrich.AgentEventTypeKey), "tool_call")), testScope)

	err := logger.Log(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.ErrorContains(t, err, "reserved namespace")
	require.Empty(t, *published)
}

func TestLogRefusesARecordThatBreaksTheIngestContract(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())

	err := testLogger(publisher).Log(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0), log.String("payload", strings.Repeat("x", 4*constants.MiB))))

	require.ErrorContains(t, err, "exceeds maximum size")
	require.Empty(t, *published)
}

func TestLogReturnsAPublishFailure(t *testing.T) {
	t.Parallel()

	publisher, _ := capture(t, gcp.NewErrPublishResult(errors.New("pubsub unavailable")))

	err := testLogger(publisher).Log(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0)))

	require.ErrorContains(t, err, "pubsub unavailable")
}

func TestLogKeepsEmptyAndNilBytesValues(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())

	err := testLogger(publisher).Log(authenticated(t.Context()), toolCallRecord(time.Unix(1_700_000_000, 0),
		log.Slice("list", log.StringValue("a"), log.Value{}),
		log.Bytes("raw", nil),
	))

	require.NoError(t, err)
	require.Len(t, *published, 1)
}

func TestUnixNanoSaturatesInsteadOfOverflowing(t *testing.T) {
	t.Parallel()

	require.Zero(t, unixNano(time.Time{}))
	require.Zero(t, unixNano(time.Unix(-1, 0)))
	require.Equal(t, uint64(1_700_000_000_000_000_001), unixNano(time.Unix(1_700_000_000, 1)))
	require.Equal(t, uint64(math.MaxUint64), unixNano(time.Unix(1<<62, 0)))
}

// ackOnRelease acks once release closes, or fails once the publish context is done.
type ackOnRelease struct {
	done    <-chan struct{}
	err     func() error
	release <-chan struct{}
}

func (r ackOnRelease) Ready() <-chan struct{} { return r.release }

func (r ackOnRelease) Get(ctx context.Context) (string, error) {
	select {
	case <-r.release:
		return "server-id", nil
	case <-r.done:
		return "", fmt.Errorf("publish context: %w", r.err())
	case <-ctx.Done():
		return "", fmt.Errorf("get context: %w", ctx.Err())
	}
}

// heldPublisher signals each Publish on arrived and acks it once release closes.
type heldPublisher struct {
	arrived  chan struct{}
	contexts chan context.Context
	release  chan struct{}
}

func (p *heldPublisher) Publish(ctx context.Context, _ *otelv1.InboundLogRecord, _ ...gcp.PublishOption) gcp.PublishResult {
	p.contexts <- ctx
	p.arrived <- struct{}{}
	return ackOnRelease{done: ctx.Done(), err: ctx.Err, release: p.release}
}

func (*heldPublisher) Stop(context.Context) error { return nil }

func newHeldPublisher(capacity int) *heldPublisher {
	return &heldPublisher{arrived: make(chan struct{}, capacity), contexts: make(chan context.Context, capacity), release: make(chan struct{})}
}

func TestLogStillPublishesWhenTheCallerIsCancelled(t *testing.T) {
	t.Parallel()

	publisher := newHeldPublisher(1)
	ctx, cancel := context.WithCancel(authenticated(t.Context()))
	defer cancel()

	result := make(chan error, 1)
	go func() { result <- testLogger(publisher).Log(ctx, toolCallRecord(time.Now())) }()
	<-publisher.arrived
	cancel()

	// Cancellation propagates synchronously, so a still-live context here means it was detached.
	require.NoError(t, (<-publisher.contexts).Err(), "the caller's cancellation reached the publish")
	close(publisher.release)
	require.NoError(t, <-result)
}

func TestLogStampsARecordWithNoTimestampSoDistinctRecordsGetDistinctIDs(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger := testLogger(publisher)
	ctx := authenticated(t.Context())

	require.NoError(t, logger.Log(ctx, toolCallRecord(time.Time{})))
	require.NotZero(t, (*published)[0].GetTimeUnixNano())

	// Two calls in the same clock tick may legitimately share a timestamp.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		require.NoError(c, logger.Log(ctx, toolCallRecord(time.Time{})))
		last := (*published)[len(*published)-1]
		assert.NotEqual(c, (*published)[0].GetRecordId(), last.GetRecordId())
	}, time.Second, time.Millisecond)
}

type typedError struct{}

func (typedError) Error() string { return "tool exploded" }

func TestLogMapsTheRecordsErrorToExceptionAttributes(t *testing.T) {
	t.Parallel()

	publisher, published := capture(t, gcp.NewSuccessPublishResult())
	logger := testLogger(publisher)
	ctx := authenticated(t.Context())

	record := toolCallRecord(time.Unix(1_700_000_000, 0))
	record.SetErr(fmt.Errorf("call failed: %w", typedError{}))
	require.NoError(t, logger.Log(ctx, record))

	stated := toolCallRecord(time.Unix(1_700_000_000, 0), log.String("exception.message", "stated"))
	stated.SetErr(errors.New("ignored"))
	require.NoError(t, logger.Log(ctx, stated))

	require.Len(t, *published, 2)
	require.Equal(t, "call failed: tool exploded", attributeValue((*published)[0], "exception.message"))
	require.Equal(t, "github.com/speakeasy-api/gram/server/internal/otelpub.typedError", attributeValue((*published)[0], "exception.type"))
	require.Equal(t, "stated", attributeValue((*published)[1], "exception.message"), "a stated exception wins over the record's error")
	require.Empty(t, attributeValue((*published)[1], "exception.type"))
}

func TestLogReturnsAnErrorWhenThePublishOutlastsTheTimeout(t *testing.T) {
	t.Parallel()

	publisher := newHeldPublisher(1)
	logger := testLogger(publisher)
	logger.timeout = 50 * time.Millisecond

	err := logger.Log(authenticated(t.Context()), toolCallRecord(time.Now()))

	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestConcurrentLogsPublishInParallel(t *testing.T) {
	t.Parallel()

	const calls = 2
	publisher := newHeldPublisher(calls)
	logger := testLogger(publisher)
	ctx := authenticated(t.Context())

	errs := make(chan error, calls)
	for range calls {
		go func() { errs <- logger.Log(ctx, toolCallRecord(time.Now())) }()
	}
	for range calls {
		select {
		case <-publisher.arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("Log calls were serialized: a second publish never started while the first was in flight")
		}
	}
	close(publisher.release)
	for range calls {
		require.NoError(t, <-errs)
	}
}

func TestCloseWaitsForInFlightLogsAndRefusesLaterOnes(t *testing.T) {
	t.Parallel()

	publisher := newHeldPublisher(2)
	logger := testLogger(publisher)
	ctx := authenticated(t.Context())

	inFlight := make(chan error, 1)
	go func() { inFlight <- logger.Log(ctx, toolCallRecord(time.Now())) }()
	<-publisher.arrived

	closed := make(chan struct{})
	go func() {
		logger.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned while a Log call was still publishing")
	case <-time.After(100 * time.Millisecond):
	}
	close(publisher.release)
	<-closed
	require.NoError(t, <-inFlight)

	require.ErrorIs(t, logger.Log(ctx, toolCallRecord(time.Now())), ErrClosed)
	require.Empty(t, publisher.arrived, "a Log after Close publishes nothing")
}

type namedError struct{}

func (namedError) Error() string     { return "named" }
func (namedError) ErrorType() string { return "custom.Named" }

func TestErrorTypeLooksThroughWrappingForANamedType(t *testing.T) {
	t.Parallel()

	require.Equal(t, "custom.Named", errorType(namedError{}))
	require.Equal(t, "custom.Named", errorType(fmt.Errorf("outer: %w", namedError{})))
	require.Equal(t, "custom.Named", errorType(fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", namedError{}))))
}
