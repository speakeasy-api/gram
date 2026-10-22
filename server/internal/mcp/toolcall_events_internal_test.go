package mcp

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
	"github.com/speakeasy-api/gram/server/internal/otelpub"
)

// publishedToolCallRecord publishes record for call and returns what reached
// the publisher and what was logged.
func publishedToolCallRecord(t *testing.T, result gcp.PublishResult, call toolCall, record log.Record) (*otelv1.InboundLogRecord, string) {
	t.Helper()

	var published []*otelv1.InboundLogRecord
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.InboundLogRecord)
		require.True(t, ok)
		published = append(published, record)
	}).Return(result).Once()

	var warnings bytes.Buffer
	logger := otelpub.NewLogger(publisher, resource.NewSchemaless(semconv.ServiceNameKey.String("gram-server")), dialect.GatewayLogScope)
	publishToolCallRecord(t.Context(), logger, slog.New(slog.NewTextHandler(&warnings, nil)), call, record)
	require.Len(t, published, 1)
	return published[0], warnings.String()
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

func TestToolCallRecordIsASemconvToolCallUnderTheCallID(t *testing.T) {
	t.Parallel()

	start := time.Unix(1_700_000_000, 0)
	call := toolCall{
		id:             "call-1",
		start:          start,
		organizationID: "org-1",
		projectID:      "project-1",
		sessionID:      "session-1",
		toolName:       "list_repos",
		toolURN:        "tools:http:github:list_repos",
		toolsetSlug:    "github",
		clientName:     "claude-code",
		externalUserID: "ext-user-1",
		userID:         "user-1",
		userEmail:      "dev@example.com",
	}

	record, warnings := publishedToolCallRecord(t, gcp.NewSuccessPublishResult(), call, call.record(start.Add(1500*time.Millisecond), http.StatusOK, false, nil))
	require.Empty(t, warnings)

	require.Equal(t, "call-1", record.GetRecordId(), "the tool call id is the record id")
	require.Equal(t, "org-1", record.GetProvenance().GetOrganizationId())
	require.Equal(t, "project-1", record.GetProvenance().GetProjectId())
	require.Equal(t, dialect.GatewayLogScope, record.GetScope().GetName())
	require.Equal(t, "execute_tool", record.GetEventName())
	require.Equal(t, uint64(1_700_000_001_500_000_000), record.GetTimeUnixNano())

	require.Equal(t, "execute_tool", recordStringAttribute(record, "gen_ai.operation.name"))
	require.Equal(t, "tools/call", recordStringAttribute(record, "mcp.method.name"))
	require.Equal(t, "call-1", recordStringAttribute(record, "gen_ai.tool.call.id"))
	require.Equal(t, "list_repos", recordStringAttribute(record, "gen_ai.tool.name"))
	require.Equal(t, "session-1", recordStringAttribute(record, "mcp.session.id"))
	require.Equal(t, "ext-user-1", recordStringAttribute(record, "user.id"))
	require.Equal(t, "dev@example.com", recordStringAttribute(record, "user.email"))
	require.Equal(t, "user-1", recordStringAttribute(record, "gram.auth.user_id"))
	require.Equal(t, "github", recordStringAttribute(record, "gram.toolset.slug"))
	require.Equal(t, "claude-code", recordStringAttribute(record, "gram.mcp.client.name"))
	require.Equal(t, "session-1", recordStringAttribute(record, "gen_ai.conversation.id"), "the MCP session is the conversation")
	require.False(t, recordHasAttribute(record, "gram.chat.id"), "an unknown value leaves its attribute off")
	require.False(t, recordHasAttribute(record, "error.type"))
	require.False(t, recordHasAttribute(record, "error.message"))
	require.False(t, recordHasAttribute(record, "gram.authorization.actor.type"), "a person made the call, so no agent is named")
	require.False(t, recordHasAttribute(record, "gram.authorization.actor.id"))
	require.Equal(t, enrich.InternalProvenanceSource, record.GetProvenance().GetSource(), "Speakeasy emitted the record itself")
	for _, kv := range record.GetAttributes() {
		switch kv.GetKey() {
		case "http.response.status_code":
			require.Equal(t, int64(http.StatusOK), kv.GetValue().GetIntValue())
		case "gram.tool_call.duration":
			require.InDelta(t, 1.5, kv.GetValue().GetDoubleValue(), 0.0001)
		}
	}
	require.True(t, recordHasAttribute(record, "gram.tool_call.duration"))
}

func TestToolCallRecordNamesTheAgentTheCallRanAs(t *testing.T) {
	t.Parallel()

	call := toolCall{id: "call-5", start: time.Unix(1_700_000_000, 0), organizationID: "org-1", projectID: "project-1", agentID: "agent-1"}

	record, _ := publishedToolCallRecord(t, gcp.NewSuccessPublishResult(), call, call.record(call.start, http.StatusOK, false, nil))
	require.Equal(t, "agent", recordStringAttribute(record, "gram.authorization.actor.type"))
	require.Equal(t, "agent-1", recordStringAttribute(record, "gram.authorization.actor.id"))
}

func TestToolCallRecordCarriesTheGatewaysOwnFailure(t *testing.T) {
	t.Parallel()

	call := toolCall{id: "call-2", start: time.Unix(1_700_000_000, 0), organizationID: "org-1", projectID: "project-1"}
	failure := oops.E(oops.CodeForbidden, errors.New("policy rule 7 matched"), "blocked by policy")

	record, _ := publishedToolCallRecord(t, gcp.NewSuccessPublishResult(), call, call.record(call.start, http.StatusForbidden, false, failure))
	require.Equal(t, string(oops.CodeForbidden), recordStringAttribute(record, "error.type"))
	require.Equal(t, "blocked by policy", recordStringAttribute(record, "error.message"), "the client's message, never the cause")
}

func TestToolCallRecordFollowsAPassthroughResultsOwnVerdict(t *testing.T) {
	t.Parallel()

	call := toolCall{id: "call-4", start: time.Unix(1_700_000_000, 0), organizationID: "org-1", projectID: "project-1"}

	record, _ := publishedToolCallRecord(t, gcp.NewSuccessPublishResult(), call, call.record(call.start, http.StatusOK, true, nil))
	require.Equal(t, "tool_error", recordStringAttribute(record, "error.type"), "the upstream said isError, so the client read an error")
	require.False(t, recordHasAttribute(record, "error.message"), "the result document is the record of it")
}

func TestToolCallRecordPublishFailureIsOnlyAWarning(t *testing.T) {
	t.Parallel()

	call := toolCall{id: "call-3", start: time.Unix(1_700_000_000, 0), organizationID: "org-1", projectID: "project-1"}

	_, warnings := publishedToolCallRecord(t, gcp.NewErrPublishResult(errors.New("pubsub unavailable")), call, call.record(call.start, http.StatusOK, false, nil))
	require.Equal(t, 1, strings.Count(warnings, "tool call record was not published"))
	require.Contains(t, warnings, "pubsub unavailable")
	require.Contains(t, warnings, "call-3")
}

func TestToolCallRecordConversationFallsBackToTheChat(t *testing.T) {
	t.Parallel()

	call := toolCall{id: "call-5", start: time.Unix(1_700_000_000, 0), chatID: "chat-1"}

	record := call.record(call.start, http.StatusOK, false, nil)
	var conversationID string
	record.WalkAttributes(func(kv log.KeyValue) bool {
		if kv.Key == "gen_ai.conversation.id" {
			conversationID = kv.Value.AsString()
		}
		return true
	})
	require.Equal(t, "chat-1", conversationID)
}

func TestToolCallErrorTypeIsEmptyOnlyForASuccess(t *testing.T) {
	t.Parallel()

	require.Empty(t, toolCallErrorType(http.StatusOK, false, nil))
	require.Equal(t, "502", toolCallErrorType(http.StatusBadGateway, false, nil))
	require.Equal(t, "tool_error", toolCallErrorType(http.StatusOK, true, nil))
	require.Equal(t, string(oops.CodeUnexpected), toolCallErrorType(http.StatusInternalServerError, false, oops.E(oops.CodeUnexpected, nil, "boom")))
}

func TestToolCallOutcomeFollowsTheIsErrorRule(t *testing.T) {
	t.Parallel()

	require.Equal(t, dialect.OutcomeOK, toolCallOutcome(http.StatusOK, false))
	require.Equal(t, dialect.OutcomeOK, toolCallOutcome(http.StatusNoContent, false))
	require.Equal(t, dialect.OutcomeError, toolCallOutcome(http.StatusBadRequest, false))
	require.Equal(t, dialect.OutcomeError, toolCallOutcome(http.StatusBadGateway, false))
	require.Equal(t, dialect.OutcomeError, toolCallOutcome(http.StatusFound, false), "a redirect is not a tool result, so the client sees isError and so does the row")
	require.Equal(t, dialect.OutcomeError, toolCallOutcome(http.StatusOK, true), "a passthrough result's own isError is the verdict the client reads")
}

func TestMCPResultIsErrorReadsOnlyAResultDocument(t *testing.T) {
	t.Parallel()

	require.True(t, mcpResultIsError([]byte(`{"content":[{"type":"text","text":"boom"}],"isError":true}`)))
	require.False(t, mcpResultIsError([]byte(`{"content":[{"type":"text","text":"ok"}]}`)))
	require.False(t, mcpResultIsError([]byte(`{"content":[],"isError":false}`)))
	require.False(t, mcpResultIsError([]byte(`not json`)))
	require.False(t, mcpResultIsError(nil))
}

func TestToolCallCallerIsNamedOnlyInsideTheToolsOrganization(t *testing.T) {
	t.Parallel()

	inside := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-1"})
	outside := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-2"})
	payload := &mcpInputs{authenticated: true, userID: "user-1"}

	userID, email := toolCallCaller(inside, payload, "org-1", "dev@example.com")
	require.Equal(t, "user-1", userID)
	require.Equal(t, "dev@example.com", email)

	userID, email = toolCallCaller(outside, payload, "org-1", "dev@example.com")
	require.Empty(t, userID, "an outside caller on a public MCP is identified by its external user id alone")
	require.Empty(t, email)

	userID, email = toolCallCaller(inside, &mcpInputs{authenticated: false, userID: "user-1"}, "org-1", "dev@example.com")
	require.Empty(t, userID)
	require.Empty(t, email)

	userID, email = toolCallCaller(t.Context(), payload, "org-1", "dev@example.com")
	require.Empty(t, userID, "no auth context names nobody")
	require.Empty(t, email)
}
