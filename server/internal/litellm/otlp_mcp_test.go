package litellm

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// mcpGatewayToolCallMetadata mirrors the JSON LiteLLM stringifies into the
// metadata.mcp_tool_call_metadata span attribute. The arguments value is a
// sentinel so tests can prove the raw payload never reaches storage.
const mcpGatewayToolCallMetadata = `{"name":"create_issue","namespaced_tool_name":"github/create_issue","mcp_server_name":"github","mcp_server_resource":"https://api.githubcopilot.com","arguments":{"title":"fixture-sensitive-argument"}}`

func TestParseMCPToolCallMetadata(t *testing.T) {
	t.Parallel()

	t.Run("it decodes the server identity fields", func(t *testing.T) {
		t.Parallel()
		md, ok := parseMCPToolCallMetadata(mcpGatewayToolCallMetadata)
		require.True(t, ok)
		require.Equal(t, "create_issue", md.Name)
		require.Equal(t, "github/create_issue", md.NamespacedToolName)
		require.Equal(t, "github", md.MCPServerName)
		require.Equal(t, "https://api.githubcopilot.com", md.MCPServerResource)
	})

	t.Run("it rejects payloads that are not JSON objects", func(t *testing.T) {
		t.Parallel()
		for _, raw := range []string{"not json", "", "   ", "null", "[]", `"github"`, "42"} {
			_, ok := parseMCPToolCallMetadata(raw)
			require.False(t, ok, "raw=%q", raw)
		}
	})

	t.Run("it rejects objects that identify no server", func(t *testing.T) {
		t.Parallel()
		_, ok := parseMCPToolCallMetadata(`{"name":"create_issue","arguments":{}}`)
		require.False(t, ok)
		_, ok = parseMCPToolCallMetadata(`{"mcp_server_name":"  ","mcp_server_resource":""}`)
		require.False(t, ok)
	})

	t.Run("it rejects oversized payloads", func(t *testing.T) {
		t.Parallel()
		padding := make([]byte, maxOTLPAttributeBytes)
		for i := range padding {
			padding[i] = 'a'
		}
		_, ok := parseMCPToolCallMetadata(`{"mcp_server_name":"github","arguments":"` + string(padding) + `"}`)
		require.False(t, ok)
	})
}

func TestMCPSpanAttributes(t *testing.T) {
	t.Parallel()

	t.Run("it keys the server on the upstream origin when reported", func(t *testing.T) {
		t.Parallel()
		got := mcpSpanAttributes(mcpToolCallMetadata{Name: "create_issue", NamespacedToolName: "github/create_issue", MCPServerName: "github", MCPServerResource: "https://api.githubcopilot.com"})
		require.Equal(t, "https://api.githubcopilot.com", got[attr.MCPServerURLKey])
		require.Equal(t, "https://api.githubcopilot.com", got[attr.MCPMatchKey])
		require.Equal(t, "github", got[attr.ToolCallSourceKey])
		require.Len(t, got, 3)
	})

	t.Run("it falls back to the tool namespace identity when the origin is missing", func(t *testing.T) {
		t.Parallel()
		got := mcpSpanAttributes(mcpToolCallMetadata{Name: "", NamespacedToolName: "", MCPServerName: "internal_docs", MCPServerResource: ""})
		require.Equal(t, "mcp-tool://internal_docs", got[attr.MCPServerURLKey])
		require.Equal(t, "mcp-tool://internal_docs", got[attr.MCPMatchKey])
		require.Equal(t, "internal_docs", got[attr.ToolCallSourceKey])
	})

	t.Run("it falls back to the tool namespace identity when the origin has no host", func(t *testing.T) {
		t.Parallel()
		got := mcpSpanAttributes(mcpToolCallMetadata{Name: "", NamespacedToolName: "", MCPServerName: "GitHub", MCPServerResource: "not a url"})
		require.Equal(t, "mcp-tool://github", got[attr.MCPServerURLKey])
		require.Equal(t, "GitHub", got[attr.ToolCallSourceKey])
	})

	t.Run("it derives the source from the origin host when the name is missing", func(t *testing.T) {
		t.Parallel()
		got := mcpSpanAttributes(mcpToolCallMetadata{Name: "", NamespacedToolName: "", MCPServerName: "", MCPServerResource: "https://mcp.example.test:8443"})
		require.Equal(t, "https://mcp.example.test:8443", got[attr.MCPServerURLKey])
		require.Equal(t, "mcp.example.test:8443", got[attr.ToolCallSourceKey])
	})

	t.Run("it yields nothing when no identity can be derived", func(t *testing.T) {
		t.Parallel()
		// A name with whitespace cannot be carried as a URL host, so there is
		// no synthetic identity either.
		got := mcpSpanAttributes(mcpToolCallMetadata{Name: "", NamespacedToolName: "", MCPServerName: "Internal Docs", MCPServerResource: ""})
		require.Empty(t, got[attr.MCPServerURLKey])
		require.Nil(t, got)
		require.Nil(t, mcpSpanAttributes(mcpToolCallMetadata{Name: "", NamespacedToolName: "", MCPServerName: "", MCPServerResource: ""}))
	})
}

// mcpGatewaySpanRequest builds a single-span export shaped like the span
// LiteLLM's MCP gateway emits for one upstream tool call.
func mcpGatewaySpanRequest(metadataKey, metadata string) *otlpExportRequest {
	model := "MCP: create_issue"
	email := "dev@example.com"
	attributes := []otlpKeyValue{
		{Key: "gen_ai.request.model", Value: otlpAnyValue{StringValue: &model}},
		{Key: "metadata.user_api_key_user_email", Value: otlpAnyValue{StringValue: &email}},
	}
	if metadataKey != "" {
		attributes = append(attributes, otlpKeyValue{Key: metadataKey, Value: otlpAnyValue{StringValue: &metadata}})
	}
	return &otlpExportRequest{ResourceSpans: []otlpResourceSpans{{
		Resource: nil,
		ScopeSpans: []otlpScopeSpans{{
			Scope: nil,
			Spans: []otlpSpan{{
				TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", ParentSpanID: "", Name: "litellm_request", Kind: jsonInt32(tracev1.Span_SPAN_KIND_CLIENT),
				StartTimeUnixNano: 1785542401000000000, EndTimeUnixNano: 1785542401250000000,
				Attributes:             attributes,
				DroppedAttributesCount: 0, Status: nil,
			}},
		}},
	}}}
}

func TestTraceLogParamsStampsMCPGatewayAttributes(t *testing.T) {
	t.Parallel()

	service, _ := newTraceTestService(t, fixedAuthorizer{authCtx: testAuthContext()}, testenv.NewMeterProvider(t), func(context.Context, []telemetry.LogParams) error { return nil })

	t.Run("it stamps the server identity and actor from metadata.mcp_tool_call_metadata", func(t *testing.T) {
		t.Parallel()
		params := service.traceLogParams(t.Context(), mcpGatewaySpanRequest("metadata.mcp_tool_call_metadata", mcpGatewayToolCallMetadata), "org-id", uuid.NewString())
		require.Len(t, params, 1)
		row := params[0]
		require.Equal(t, "https://api.githubcopilot.com", row.Attributes[attr.MCPServerURLKey])
		require.Equal(t, "https://api.githubcopilot.com", row.Attributes[attr.MCPMatchKey])
		require.Equal(t, "github", row.Attributes[attr.ToolCallSourceKey])
		require.Equal(t, "litellm", row.Attributes[attr.HookSourceKey])
		require.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", row.Attributes[attr.TraceIDKey])
		require.Equal(t, "dev@example.com", row.UserInfo.Email())
		require.Empty(t, row.UserInfo.UserID())
		// The span is not model usage, so the model attribute is stripped like
		// any other non-model span.
		require.NotContains(t, row.Attributes, attr.GenAIRequestModelKey)
		// The raw payload carries tool arguments and must never be persisted.
		for key := range row.Attributes {
			require.NotContains(t, string(key), "mcp_tool_call_metadata")
		}
		for _, value := range row.Attributes {
			if text, ok := value.(string); ok {
				require.NotContains(t, text, "fixture-sensitive-argument")
			}
		}
	})

	t.Run("it accepts the litellm.metadata prefix", func(t *testing.T) {
		t.Parallel()
		params := service.traceLogParams(t.Context(), mcpGatewaySpanRequest("litellm.metadata.mcp_tool_call_metadata", mcpGatewayToolCallMetadata), "org-id", uuid.NewString())
		require.Len(t, params, 1)
		require.Equal(t, "https://api.githubcopilot.com", params[0].Attributes[attr.MCPServerURLKey])
		require.Equal(t, "github", params[0].Attributes[attr.ToolCallSourceKey])
		require.Equal(t, "dev@example.com", params[0].UserInfo.Email())
	})

	t.Run("it uses the tool namespace identity for servers known only by name", func(t *testing.T) {
		t.Parallel()
		params := service.traceLogParams(t.Context(), mcpGatewaySpanRequest("metadata.mcp_tool_call_metadata", `{"name":"search","namespaced_tool_name":"internal_docs/search","mcp_server_name":"internal_docs","mcp_server_resource":""}`), "org-id", uuid.NewString())
		require.Len(t, params, 1)
		require.Equal(t, "mcp-tool://internal_docs", params[0].Attributes[attr.MCPServerURLKey])
		require.Equal(t, "mcp-tool://internal_docs", params[0].Attributes[attr.MCPMatchKey])
		require.Equal(t, "internal_docs", params[0].Attributes[attr.ToolCallSourceKey])
	})

	t.Run("it leaves spans without MCP metadata untouched", func(t *testing.T) {
		t.Parallel()
		params := service.traceLogParams(t.Context(), mcpGatewaySpanRequest("", ""), "org-id", uuid.NewString())
		require.Len(t, params, 1)
		require.NotContains(t, params[0].Attributes, attr.MCPServerURLKey)
		require.NotContains(t, params[0].Attributes, attr.MCPMatchKey)
		require.NotContains(t, params[0].Attributes, attr.ToolCallSourceKey)
		require.Empty(t, params[0].UserInfo.Email())
	})

	t.Run("it ignores malformed MCP metadata", func(t *testing.T) {
		t.Parallel()
		params := service.traceLogParams(t.Context(), mcpGatewaySpanRequest("metadata.mcp_tool_call_metadata", "not json"), "org-id", uuid.NewString())
		require.Len(t, params, 1)
		require.NotContains(t, params[0].Attributes, attr.MCPServerURLKey)
		require.Empty(t, params[0].UserInfo.Email())
	})
}

func TestTracePersistenceStampsMCPGatewayAttributes(t *testing.T) {
	t.Parallel()

	ctx, instance := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	instance.service.auth = fixedAuthorizer{authCtx: authCtx}
	mux := mountedTraceMux(instance.service)
	projectID := authCtx.ProjectID.String()
	traceID := "4bf92f3577b34da6a3ce929d0e0e4736"

	body := fmt.Sprintf(`{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":%q,"spanId":"00f067aa0ba902b7","name":"litellm_request","kind":3,"startTimeUnixNano":"1785542401000000000","endTimeUnixNano":"1785542401250000000","attributes":[{"key":"gen_ai.request.model","value":{"stringValue":"MCP: create_issue"}},{"key":"metadata.user_api_key_user_email","value":{"stringValue":"dev@example.com"}},{"key":"metadata.mcp_tool_call_metadata","value":{"stringValue":%q}}]}]}]}]}`, traceID, mcpGatewayToolCallMetadata)
	require.Equal(t, http.StatusAccepted, serveTraceRequest(t, mux, []byte(body), "application/json", "", "fixture-key", "fixture-project").Code)
	require.NoError(t, instance.service.traces.Shutdown(t.Context()))

	query := telemetryrepo.New(instance.chConn)
	var logs []telemetryrepo.TelemetryLog
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		testenv.FlushClickHouseAsyncInserts(t, instance.chConn)
		var err error
		logs, err = query.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: projectID,
			TimeStart:     time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC).UnixNano(),
			TimeEnd:       time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC).UnixNano(),
			GramURNs:      []string{litellmOTLPResourceURN},
			SortOrder:     "asc",
			Cursor:        "",
			Limit:         10,
		})
		assert.NoError(collect, err)
		assert.Len(collect, logs, 1)
	}, 10*time.Second, 50*time.Millisecond)

	row := logs[0]
	require.NotNil(t, row.TraceID)
	require.Equal(t, traceID, *row.TraceID)
	require.Equal(t, "https://api.githubcopilot.com", gjson.Get(row.Attributes, "gram.mcp.server_url").String())
	require.Equal(t, "https://api.githubcopilot.com", gjson.Get(row.Attributes, "gram.mcp.match").String())
	require.Equal(t, "github", gjson.Get(row.Attributes, "gram.tool_call.source").String())
	require.Equal(t, "litellm", gjson.Get(row.Attributes, "gram.hook.source").String())
	require.False(t, gjson.Get(row.Attributes, "metadata.mcp_tool_call_metadata").Exists())
	require.NotContains(t, row.Attributes, "mcp_tool_call_metadata")
	require.NotContains(t, row.Attributes, "fixture-sensitive-argument")

	var toolSource, hookSource, userEmail string
	require.NoError(t, instance.chConn.QueryRow(ctx,
		`SELECT tool_source, hook_source, user_email FROM telemetry_logs WHERE gram_project_id = ? AND trace_id = ?`,
		projectID, traceID,
	).Scan(&toolSource, &hookSource, &userEmail))
	require.Equal(t, "github", toolSource)
	require.Equal(t, "litellm", hookSource)
	require.Equal(t, "dev@example.com", userEmail)

	var summaryServerURL, summaryMatch, summaryToolSource, summaryHookSource, summaryUserEmail string
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		err := instance.chConn.QueryRow(ctx,
			`SELECT max(mcp_server_url), max(mcp_match), max(tool_source), max(hook_source), max(user_email) FROM trace_summaries WHERE gram_project_id = ? AND trace_id = ?`,
			projectID, traceID,
		).Scan(&summaryServerURL, &summaryMatch, &summaryToolSource, &summaryHookSource, &summaryUserEmail)
		assert.NoError(collect, err)
		assert.Equal(collect, "https://api.githubcopilot.com", summaryServerURL)
	}, 10*time.Second, 50*time.Millisecond)
	require.Equal(t, "https://api.githubcopilot.com", summaryMatch)
	require.Equal(t, "github", summaryToolSource)
	require.Equal(t, "litellm", summaryHookSource)
	require.Equal(t, "dev@example.com", summaryUserEmail)
}
