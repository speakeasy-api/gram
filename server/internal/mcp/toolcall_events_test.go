package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// hostedToolFixture is a public hosted MCP server with one HTTP tool.
type hostedToolFixture struct {
	slug     string
	serverID uuid.UUID
	upstream *httptest.Server
	status   atomic.Int32
	// contentType the gateway cannot format fails the call after the tool ran.
	contentType atomic.Pointer[string]
}

func seedHostedTool(t *testing.T, ctx context.Context, ti *testInstance, toolName string) *hostedToolFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := "events-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, slug)
	server := createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.Nil)
	addHTTPTools(t, ctx, ti, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID, toolName)

	fixture := &hostedToolFixture{slug: slug, serverID: server.ID, upstream: nil}
	fixture.status.Store(http.StatusOK)
	fixture.contentType.Store(new("application/json"))
	fixture.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", *fixture.contentType.Load())
		w.WriteHeader(int(fixture.status.Load()))
		_, _ = w.Write([]byte(`{"repos":[]}`))
	}))
	t.Cleanup(fixture.upstream.Close)
	return fixture
}

func (f *hostedToolFixture) call(t *testing.T, ctx context.Context, ti *testInstance, toolName string) (isError bool) {
	t.Helper()

	body := makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": map[string]any{},
		"_meta": map[string]any{
			"io.modelcontextprotocol/clientInfo": map[string]any{"name": "claude-code", "version": "2.0.1"},
		},
	})
	response, err := servePublicHTTP(t, ctx, ti, f.slug, body, "", map[string]string{
		"Mcp-Test-Server-Url": f.upstream.URL,
		"Mcp-Session-Id":      "session-1",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	_, isError = metaToolResultText(t, decodeRPCResponse(t, response))
	return isError
}

func (f *hostedToolFixture) callExpectingFailure(t *testing.T, ctx context.Context, ti *testInstance, toolName string) json.RawMessage {
	t.Helper()

	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": toolName, "arguments": map[string]any{}})
	response, err := servePublicHTTP(t, ctx, ti, f.slug, body, "", map[string]string{
		"Mcp-Test-Server-Url": f.upstream.URL,
		"Mcp-Session-Id":      "session-1",
	})
	require.NoError(t, err)
	envelope := decodeRPCResponse(t, response)
	require.Contains(t, envelope, "error", "body=%s", response.Body.String())
	return envelope["error"]
}

func toolCallRecordAttribute(record *otelv1.InboundLogRecord, key string) *otelv1.InboundLogRecord_AnyValue {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() == key {
			return kv.GetValue()
		}
	}
	return nil
}

// awaitToolCallRecord waits for the call's one record, published off the response path.
func awaitToolCallRecord(t *testing.T, ti *testInstance) *otelv1.InboundLogRecord {
	t.Helper()

	var records []*otelv1.InboundLogRecord
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		records = ti.toolCallRecords.all()
		assert.NotEmpty(c, records)
	}, 5*time.Second, 10*time.Millisecond)
	require.Len(t, records, 1)
	return records[0]
}

func TestToolsCall_EmitsOneExecuteToolRecordForTheCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	fixture := seedHostedTool(t, ctx, ti, "list_repos")

	require.False(t, fixture.call(t, ctx, ti, "list_repos"))

	record := awaitToolCallRecord(t, ti)
	callID := toolCallRecordAttribute(record, "gen_ai.tool.call.id").GetStringValue()
	require.NotEmpty(t, callID)
	require.Equal(t, callID, record.GetRecordId(), "the record travels under the tool call id")
	require.Equal(t, "execute_tool", record.GetEventName())
	require.Equal(t, "execute_tool", toolCallRecordAttribute(record, "gen_ai.operation.name").GetStringValue())
	require.Equal(t, "tools/call", toolCallRecordAttribute(record, "mcp.method.name").GetStringValue())
	require.Equal(t, "list_repos", toolCallRecordAttribute(record, "gen_ai.tool.name").GetStringValue())
	require.Equal(t, "session-1", toolCallRecordAttribute(record, "mcp.session.id").GetStringValue())
	require.Equal(t, "session-1", toolCallRecordAttribute(record, "gen_ai.conversation.id").GetStringValue())
	require.Equal(t, authCtx.ActiveOrganizationID, record.GetProvenance().GetOrganizationId())
	require.Equal(t, authCtx.ProjectID.String(), record.GetProvenance().GetProjectId())
	require.Equal(t, dialect.GatewayLogScope, record.GetScope().GetName())
	require.Equal(t, fixture.slug, toolCallRecordAttribute(record, "gram.toolset.slug").GetStringValue())
	require.Equal(t, fixture.serverID.String(), toolCallRecordAttribute(record, string(attr.McpServerIDKey)).GetStringValue())
	require.Equal(t, "claude-code", toolCallRecordAttribute(record, "gram.mcp.client.name").GetStringValue())
	require.Equal(t, int64(http.StatusOK), toolCallRecordAttribute(record, "http.response.status_code").GetIntValue())
	require.Positive(t, toolCallRecordAttribute(record, "gram.tool_call.duration").GetDoubleValue())
	require.Nil(t, toolCallRecordAttribute(record, "error.type"), "a call that went fine has no error type")
}

func TestToolsCall_RecordSaysTheToolFailed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fixture := seedHostedTool(t, ctx, ti, "list_repos")
	fixture.status.Store(http.StatusBadGateway)

	require.True(t, fixture.call(t, ctx, ti, "list_repos"))

	record := awaitToolCallRecord(t, ti)
	require.Equal(t, "502", toolCallRecordAttribute(record, "error.type").GetStringValue())
	require.Equal(t, int64(http.StatusBadGateway), toolCallRecordAttribute(record, "http.response.status_code").GetIntValue())
	require.Nil(t, toolCallRecordAttribute(record, "error.message"), "the tool's own error document is the result, not a message about it")
}

func TestToolsCall_RecordCarriesAFailureAfterTheToolRan(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fixture := seedHostedTool(t, ctx, ti, "list_repos")
	fixture.contentType.Store(new("application/x-unknown"))

	fixture.callExpectingFailure(t, ctx, ti, "list_repos")

	record := awaitToolCallRecord(t, ti)
	require.Equal(t, string(oops.CodeUnexpected), toolCallRecordAttribute(record, "error.type").GetStringValue())
	require.Equal(t, int64(http.StatusInternalServerError), toolCallRecordAttribute(record, "http.response.status_code").GetIntValue())
	require.Equal(t, "failed format tool call result", toolCallRecordAttribute(record, "error.message").GetStringValue())
}

func TestToolsCall_SucceedsWhenItsRecordCannotBePublished(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fixture := seedHostedTool(t, ctx, ti, "list_repos")
	ti.toolCallRecords.failWith(errors.New("pubsub unavailable"))

	require.False(t, fixture.call(t, ctx, ti, "list_repos"))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 1, ti.toolCallRecords.publishAttempts())
	}, 5*time.Second, 10*time.Millisecond)
	require.Empty(t, ti.toolCallRecords.all())
}
