package mcp_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	templatesrepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func scanAttributes(recorder *tracetest.SpanRecorder, surface string) []map[attribute.Key]string {
	var events []map[attribute.Key]string
	for _, span := range recorder.Ended() {
		if span.Name() != "mcp.risk.scan" {
			continue
		}
		attrs := make(map[attribute.Key]string)
		for _, kv := range span.Attributes() {
			attrs[kv.Key] = kv.Value.Emit()
		}
		if attrs["gram.mcp.risk.scan.surface"] == surface {
			events = append(events, attrs)
		}
	}
	return events
}

func TestRiskScan_ProxiedMetaMember(t *testing.T) {
	t.Parallel()
	ctx, ti, recorder := newTestMCPServiceWithScanSpans(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "scan-meta-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, slug, issuerID)
	upstream := newRecordingUpstream(t, "ping")
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Scan member", "scan-member", 0, upstream.url)
	subject := urn.NewUserSubject("scan-user-" + uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, slug, issuerID, subject)

	rpc := executeMetaTool(t, ti, slug, bearer, "scan-member--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError)
	require.Equal(t, "pong from ping", text)
	require.Empty(t, upstream.capturedAuth())

	events := scanAttributes(recorder, mcpriskscan.SurfaceMetaMCP)
	require.Len(t, events, 1)
	require.Equal(t, memberID.String(), events[0][attr.McpServerIDKey])
	require.Equal(t, "ping", events[0][attr.ToolNameKey])
	require.Equal(t, mcpriskscan.PhaseBeforeExecution, events[0]["gram.mcp.risk.scan.phase"])
	require.Equal(t, "user_session", events[0]["gram.mcp.risk.scan.principal_kind"])
	require.Equal(t, subject.ID, events[0][attr.UserIDKey])
	require.Empty(t, scanAttributes(recorder, mcpriskscan.SurfaceHostedMCP))
	remoteEvents := scanAttributes(recorder, mcpriskscan.SurfaceRemoteMCP)
	require.Len(t, remoteEvents, 1)
	require.Equal(t, memberID.String(), remoteEvents[0][attr.McpServerIDKey])
	require.Equal(t, "ping", remoteEvents[0][attr.ToolNameKey])
}

func TestRiskScan_PromptRetrieval(t *testing.T) {
	t.Parallel()
	ctx, ti, recorder := newTestMCPServiceWithScanSpans(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := "scan-prompt-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, slug)
	server := createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.Nil)
	_, err := templatesrepo.New(ti.conn).CreateTemplate(ctx, templatesrepo.CreateTemplateParams{
		ProjectID: *authCtx.ProjectID,
		ToolUrn:   urn.NewTool(urn.ToolKindPrompt, "prompt", "scan-greeting"),
		Name:      "scan-greeting", Prompt: "Hello {{name}}",
		Description: conv.ToPGText("Greeting"),
		Arguments:   []byte(`{"type":"object","properties":{"name":{"type":"string"}}}`),
		Engine:      conv.ToPGText("mustache"), Kind: conv.ToPGText("prompt"),
		ToolsHint: nil, ToolUrnsHint: nil,
	})
	require.NoError(t, err)

	body := makeMetaRPCBody(t, "prompts/get", map[string]any{
		"name": "scan-greeting", "arguments": map[string]any{"name": "reader"},
	})
	response, err := servePublicHTTP(t, t.Context(), ti, slug, body, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code)
	rpc := decodeRPCResponse(t, response)
	var result struct {
		Description string `json:"description"`
		Messages    []struct {
			Role    string `json:"role"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(rpc["result"], &result))
	require.Equal(t, "Greeting", result.Description)
	require.Len(t, result.Messages, 1)
	require.Equal(t, "user", result.Messages[0].Role)
	require.Equal(t, "text", result.Messages[0].Content.Type)
	require.Equal(t, "Hello reader", result.Messages[0].Content.Text)

	events := scanAttributes(recorder, mcpriskscan.SurfacePromptsGet)
	require.Len(t, events, 1)
	require.Equal(t, server.ID.String(), events[0][attr.McpServerIDKey])
	require.Empty(t, events[0][attr.ToolNameKey])
	require.Equal(t, "scan-greeting", events[0]["gram.mcp.risk.scan.prompt_name"])
	require.Equal(t, mcpriskscan.PhaseBeforeRender, events[0]["gram.mcp.risk.scan.phase"])
	require.Equal(t, "false", events[0]["gram.mcp.risk.scan.identity_stamped"])
	require.Empty(t, scanAttributes(recorder, mcpriskscan.SurfaceHostedMCP))
}
