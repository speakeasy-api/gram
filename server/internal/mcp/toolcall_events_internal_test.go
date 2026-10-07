package mcp

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otel/gramotel"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// toolCallEventsFixture is a toolCallEvents over a publisher that keeps
// what reached the topic, with a clock the test moves by hand.
type toolCallEventsFixture struct {
	events    *toolCallEvents
	published *[]*otelv1.InboundLogRecord
	at        *time.Time
}

func newToolCallEventsFixture(t *testing.T, result gcp.PublishResult, identity toolCallIdentity) toolCallEventsFixture {
	t.Helper()

	var published []*otelv1.InboundLogRecord
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.InboundLogRecord)
		require.True(t, ok)
		published = append(published, record)
	}).Return(result).Maybe()

	logger := testenv.NewLogger(t)
	emitter := gramotel.NewLoggerProvider(logger, nil, publisher, resource.NewSchemaless(semconv.ServiceNameKey.String("gram-server"))).
		Logger(dialect.GramGatewayLogScope)
	at := time.Unix(1_700_000_000, 0)
	events := newToolCallEvents(logger, emitter, toolCallTenant{organizationID: "org-1", projectID: "project-1"}, identity, func() time.Time { return at })
	return toolCallEventsFixture{events: events, published: &published, at: &at}
}

func recordStringAttribute(record *otelv1.InboundLogRecord, key string) string {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func recordHasAttribute(record *otelv1.InboundLogRecord, key string) bool {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() == key {
			return true
		}
	}
	return false
}

func TestToolCallEventsPairStartedAndCompletedByTheCallID(t *testing.T) {
	t.Parallel()

	fixture := newToolCallEventsFixture(t, gcp.NewSuccessPublishResult(), toolCallIdentity{
		callID:      "call-1",
		sessionID:   "session-1",
		toolName:    "list_repos",
		toolURN:     "tools:http:github:list_repos",
		toolsetSlug: "github",
		clientName:  "claude-code",
		userEmail:   "dev@example.com",
	})

	fixture.events.started(t.Context())
	*fixture.at = fixture.at.Add(1500 * time.Millisecond)
	fixture.events.completed(t.Context(), http.StatusOK, nil)

	published := *fixture.published
	require.Len(t, published, 2)
	started, completed := published[0], published[1]

	require.Equal(t, dialect.GramToolCallStartedEvent, started.GetEventName())
	require.Equal(t, dialect.GramToolCallCompletedEvent, completed.GetEventName())
	for _, record := range published {
		require.Equal(t, "call-1", record.GetRecordId(), "the tool call id is the record id")
		require.Equal(t, "call-1", recordStringAttribute(record, "gram.tool_call.id"))
		require.Equal(t, "org-1", record.GetProvenance().GetOrganizationId())
		require.Equal(t, "project-1", record.GetProvenance().GetProjectId())
		require.Equal(t, dialect.GramGatewayLogScope, record.GetScope().GetName())
		require.Equal(t, "session-1", recordStringAttribute(record, "gram.session.id"))
		require.Equal(t, "list_repos", recordStringAttribute(record, "gram.tool.name"))
		require.Equal(t, "github", recordStringAttribute(record, "gram.toolset.slug"))
		require.Equal(t, "claude-code", recordStringAttribute(record, "gram.mcp.client.name"))
		require.Equal(t, "dev@example.com", recordStringAttribute(record, "user.email"))
		require.False(t, recordHasAttribute(record, "gram.chat.id"), "an unknown value leaves its attribute off")
	}
	require.Equal(t, uint64(1_700_000_000_000_000_000), started.GetTimeUnixNano())
	require.Equal(t, uint64(1_700_000_001_500_000_000), completed.GetTimeUnixNano())

	require.False(t, recordHasAttribute(started, "gram.outcome"))
	require.Equal(t, dialect.OutcomeOK, recordStringAttribute(completed, "gram.outcome"))
	require.False(t, recordHasAttribute(completed, "error.message"))
	for _, kv := range completed.GetAttributes() {
		switch kv.GetKey() {
		case "http.response.status_code":
			require.Equal(t, int64(http.StatusOK), kv.GetValue().GetIntValue())
		case "gram.tool_call.duration":
			require.InDelta(t, 1.5, kv.GetValue().GetDoubleValue(), 0.0001)
		}
	}
	require.True(t, recordHasAttribute(completed, "gram.tool_call.duration"))
}

func TestToolCallEventsCompletedCarriesTheGatewaysOwnFailure(t *testing.T) {
	t.Parallel()

	fixture := newToolCallEventsFixture(t, gcp.NewSuccessPublishResult(), toolCallIdentity{callID: "call-2"})
	failure := oops.E(oops.CodeForbidden, errors.New("policy rule 7 matched"), "blocked by policy")

	fixture.events.started(t.Context())
	fixture.events.completed(t.Context(), http.StatusForbidden, failure)

	completed := (*fixture.published)[1]
	require.Equal(t, dialect.OutcomeError, recordStringAttribute(completed, "gram.outcome"))
	require.Equal(t, "blocked by policy", recordStringAttribute(completed, "error.message"), "the client's message, never the cause")
}

func TestToolCallEventsNeverFailTheCall(t *testing.T) {
	t.Parallel()

	fixture := newToolCallEventsFixture(t, gcp.NewErrPublishResult(errors.New("pubsub unavailable")), toolCallIdentity{callID: "call-3"})

	// The publisher refuses both acks. Both emits still return normally:
	// the loss is counted and logged by gramotel and named by the gateway,
	// and the caller sees no error to fail the call with.
	fixture.events.started(t.Context())
	fixture.events.completed(t.Context(), http.StatusOK, nil)
	require.Len(t, *fixture.published, 2, "both records were handed to the publisher before it refused them")
}

func TestToolCallOutcomeFollowsTheIsErrorRule(t *testing.T) {
	t.Parallel()

	require.Equal(t, dialect.OutcomeOK, toolCallOutcome(http.StatusOK))
	require.Equal(t, dialect.OutcomeOK, toolCallOutcome(http.StatusNoContent))
	require.Equal(t, dialect.OutcomeError, toolCallOutcome(http.StatusBadRequest))
	require.Equal(t, dialect.OutcomeError, toolCallOutcome(http.StatusBadGateway))
	require.Equal(t, dialect.OutcomeError, toolCallOutcome(http.StatusFound), "a redirect is not a tool result, so the client sees isError and so does the row")
}
