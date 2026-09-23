package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	templatesrepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

	scanCount := 0
	for _, span := range recorder.Ended() {
		if span.Name() == "mcp.risk.scan" {
			scanCount++
		}
	}
	require.Equal(t, 1, scanCount, "proxied members must be evaluated only at the remote seam")
	remoteEvents := scanAttributes(recorder, mcpriskscan.SurfaceRemoteMCP)
	require.Len(t, remoteEvents, 1)
	require.Equal(t, memberID.String(), remoteEvents[0][attr.McpServerIDKey])
	require.Equal(t, "ping", remoteEvents[0][attr.ToolNameKey])
	require.Equal(t, mcpriskscan.MethodToolsCall, remoteEvents[0]["gram.mcp.risk.scan.method"])
	require.Equal(t, meta.ID.String(), remoteEvents[0]["gram.mcp.risk.scan.meta_mcp_server_id"])
}

func TestRiskScan_PromptRetrieval(t *testing.T) {
	t.Parallel()
	ctx, ti, recorder := newTestMCPServiceWithScanSpans(t)
	scanner := consumeRiskScanPayloads(t, ti)
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
	require.Len(t, scanner.payloads, 1)
	require.JSONEq(t, `{"name":"reader"}`, string(scanner.payloads[0]))

	events := scanAttributes(recorder, mcpriskscan.SurfaceHostedMCP)
	require.Len(t, events, 1)
	require.Equal(t, server.ID.String(), events[0][attr.McpServerIDKey])
	require.Empty(t, events[0][attr.ToolNameKey])
	require.Equal(t, mcpriskscan.MethodPromptsGet, events[0]["gram.mcp.risk.scan.method"])
	require.Equal(t, "scan-greeting", events[0]["gram.mcp.risk.scan.prompt_name"])
	require.Equal(t, mcpriskscan.PhaseRequest, events[0]["gram.mcp.risk.scan.phase"])
	require.Equal(t, "false", events[0]["gram.mcp.risk.scan.identity_stamped"])
}

type consumingRiskScan struct {
	t        *testing.T
	payloads [][]byte
}

func (s *consumingRiskScan) Observe(_ context.Context, subject mcpriskscan.Subject) {
	payload := bytes.Clone(subject.Payload.Bytes())
	s.payloads = append(s.payloads, payload)
}

func consumeRiskScanPayloads(t *testing.T, ti *testInstance) *consumingRiskScan {
	t.Helper()
	scanner := &consumingRiskScan{t: t, payloads: nil}
	noop := mcpriskscan.NewNoop(ti.tracerProvider, testenv.NewMeterProvider(t), ti.logger)
	ti.service.SetRiskScanEvaluator(mcpriskscan.PrependObserver(scanner, noop))
	return scanner
}

func TestRiskScan_HostedHTTPPreservesPayloadAndErrorResult(t *testing.T) {
	t.Parallel()
	ctx, ti, recorder := newTestMCPServiceWithScanSpans(t)
	scanner := consumeRiskScanPayloads(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := "scan-http-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, slug)
	server := createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.Nil)
	addHTTPTools(t, ctx, ti, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "scan_http")

	upstreamBodies := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		upstreamBodies <- string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"rejected","detail":"preserve this body"}`))
	}))
	t.Cleanup(upstream.Close)
	arguments := json.RawMessage(`{"body":{"message":"payload survives observation","number":9007199254740993}}`)
	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "scan_http", "arguments": arguments})
	response, err := servePublicHTTP(t, t.Context(), ti, slug, body, "", map[string]string{"Mcp-Test-Server-Url": upstream.URL})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, response))
	require.True(t, isError)
	require.JSONEq(t, `{"error":"rejected","detail":"preserve this body"}`, text)
	require.JSONEq(t, `{"message":"payload survives observation","number":9007199254740993}`, <-upstreamBodies)
	require.Len(t, scanner.payloads, 1)
	require.Equal(t, string(arguments), string(scanner.payloads[0]))

	events := scanAttributes(recorder, mcpriskscan.SurfaceHostedMCP)
	require.Len(t, events, 1)
	require.Equal(t, authCtx.ActiveOrganizationID, events[0][attr.OrganizationIDKey])
	require.Equal(t, authCtx.ProjectID.String(), events[0][attr.ProjectIDKey])
	require.Equal(t, server.ID.String(), events[0][attr.McpServerIDKey])
	require.Equal(t, toolset.ID.String(), events[0][attr.ToolsetIDKey])
	require.Equal(t, "scan_http", events[0][attr.ToolNameKey])
	require.Equal(t, mcpriskscan.MethodToolsCall, events[0]["gram.mcp.risk.scan.method"])
	require.Equal(t, mcpriskscan.PhaseRequest, events[0]["gram.mcp.risk.scan.phase"])
}

func TestRiskScan_PromptAsToolPreservesRendering(t *testing.T) {
	t.Parallel()
	ctx, ti, recorder := newTestMCPServiceWithScanSpans(t)
	scanner := consumeRiskScanPayloads(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := "scan-template-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, slug)
	toolURN := urn.NewTool(urn.ToolKindPrompt, "prompt", "scan-template")
	_, err := templatesrepo.New(ti.conn).CreateTemplate(ctx, templatesrepo.CreateTemplateParams{
		ProjectID: *authCtx.ProjectID, ToolUrn: toolURN,
		Name: "scan-template", Prompt: "Hello {{name}}",
		Description: conv.ToPGText("Greeting"),
		Arguments:   []byte(`{"type":"object","properties":{"arguments":{"type":"object","properties":{"name":{"type":"string"}}}}}`),
		Engine:      conv.ToPGText("mustache"), Kind: conv.ToPGText("prompt"),
		ToolsHint: nil, ToolUrnsHint: nil,
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID: toolset.ID, Version: 1, ToolUrns: []urn.Tool{toolURN},
		ResourceUrns: []urn.Resource{}, PredecessorID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	body := makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": "scan-template", "arguments": map[string]any{"arguments": map[string]any{"name": "reader"}},
	})
	response, err := servePublicHTTP(t, t.Context(), ti, slug, body, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, response))
	require.False(t, isError)
	require.Equal(t, "Hello reader", text)
	require.Len(t, scanner.payloads, 1)
	require.JSONEq(t, `{"arguments":{"name":"reader"}}`, string(scanner.payloads[0]))

	events := scanAttributes(recorder, mcpriskscan.SurfaceHostedMCP)
	require.Len(t, events, 1)
	require.Equal(t, "scan-template", events[0][attr.ToolNameKey])
	require.Empty(t, events[0]["gram.mcp.risk.scan.prompt_name"])
	require.Equal(t, mcpriskscan.MethodToolsCall, events[0]["gram.mcp.risk.scan.method"])
	require.Equal(t, mcpriskscan.PhaseRequest, events[0]["gram.mcp.risk.scan.phase"])
}

type riskScanResourceCaller struct {
	functions.ToolCaller
	url string
}

func (c *riskScanResourceCaller) ReadResource(ctx context.Context, input functions.RunnerResourceReadRequest) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(input.Input))
	if err != nil {
		return nil, fmt.Errorf("create resource request: %w", err)
	}
	req.Header.Set("Gram-Invoke-ID", input.InvocationID.String())
	return req, nil
}

func TestRiskScan_ResourceReadKeepsIdentityAndSyntheticBody(t *testing.T) {
	t.Parallel()
	upstreamBodies := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		upstreamBodies <- string(body)
		w.Header().Set("Gram-Invoke-ID", r.Header.Get("Gram-Invoke-ID"))
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("resource contents"))
	}))
	t.Cleanup(upstream.Close)
	caller := &riskScanResourceCaller{ToolCaller: nil, url: upstream.URL}
	ctx, ti, recorder := newTestMCPServiceWithScanSpans(t, caller)
	scanner := consumeRiskScanPayloads(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	slug := "scan-resource-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), authCtx, slug)
	server := createToolsetMcpEndpoint(t, ctx, ti.conn, projectID, toolset.ID, slug, "public", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.Nil)
	deployments := deploymentsrepo.New(ti.conn)
	deploymentID, err := deployments.InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{
		ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID,
		UserID: authCtx.UserID, IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)
	err = deployments.CreateDeploymentStatus(ctx, deploymentsrepo.CreateDeploymentStatusParams{
		DeploymentID: deploymentID, Status: "completed",
	})
	require.NoError(t, err)
	asset, err := assetsrepo.New(ti.conn).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name: "scan-resource.zip", Url: "file://scan-resource.zip", ProjectID: projectID,
		OrganizationID: authCtx.ActiveOrganizationID, Sha256: "scan-resource-asset",
		Kind: "functions", ContentType: "application/zip", ContentLength: 1,
	})
	require.NoError(t, err)
	function, err := deployments.UpsertDeploymentFunctionsAsset(ctx, deploymentsrepo.UpsertDeploymentFunctionsAssetParams{
		DeploymentID: deploymentID, AssetID: asset.ID, Name: "scan-resource", Slug: "scan-resource",
		Runtime: string(functions.RuntimeNodeJS22), MemoryMib: pgtype.Int4{Int32: 128, Valid: true},
		Scale: pgtype.Int4{Int32: 1, Valid: true},
	})
	require.NoError(t, err)
	_, err = deployments.CreateDeploymentFunctionsAccess(ctx, deploymentsrepo.CreateDeploymentFunctionsAccessParams{
		ProjectID: projectID, DeploymentID: deploymentID, FunctionID: function.ID,
		EncryptionKey: conv.NewSecret([]byte("unused-runner-key")), BearerFormat: conv.ToPGText("v1"),
	})
	require.NoError(t, err)
	resourceURI := "gram://scan/resource"
	resourceURN := urn.NewResource(urn.ResourceKindFunction, "scan-resource", resourceURI)
	_, err = deployments.CreateFunctionsResource(ctx, deploymentsrepo.CreateFunctionsResourceParams{
		DeploymentID: deploymentID, FunctionID: function.ID, ResourceUrn: resourceURN,
		ProjectID: projectID, Runtime: string(functions.RuntimeNodeJS22),
		Name: "scan-resource", Description: "Scan resource", Uri: resourceURI,
		Title: conv.ToPGText("Scan resource"), MimeType: conv.ToPGText("text/plain"),
		Variables: []byte(`{}`), Meta: []byte(`{}`),
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID: toolset.ID, Version: 1, ToolUrns: []urn.Tool{}, ResourceUrns: []urn.Resource{resourceURN},
		PredecessorID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	body := makeMetaRPCBody(t, "resources/read", map[string]any{"uri": resourceURI})
	response, err := servePublicHTTP(t, t.Context(), ti, slug, body, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	rpc := decodeRPCResponse(t, response)
	require.NotContains(t, rpc, "error")
	var result struct {
		Contents []struct {
			URI      string `json:"uri"`
			Text     string `json:"text"`
			MimeType string `json:"mimeType"`
		} `json:"contents"`
	}
	require.NoError(t, json.Unmarshal(rpc["result"], &result))
	require.Len(t, result.Contents, 1)
	require.Equal(t, resourceURI, result.Contents[0].URI)
	require.Equal(t, "resource contents", result.Contents[0].Text)
	require.Equal(t, "text/plain", result.Contents[0].MimeType)
	require.Equal(t, "{}", <-upstreamBodies)
	require.Len(t, scanner.payloads, 1)
	require.Nil(t, scanner.payloads[0], "the synthetic execution body is not caller input")

	events := scanAttributes(recorder, mcpriskscan.SurfaceHostedMCP)
	require.Len(t, events, 1)
	require.Equal(t, authCtx.ActiveOrganizationID, events[0][attr.OrganizationIDKey])
	require.Equal(t, projectID.String(), events[0][attr.ProjectIDKey])
	require.Equal(t, server.ID.String(), events[0][attr.McpServerIDKey])
	require.Equal(t, toolset.ID.String(), events[0][attr.ToolsetIDKey])
	require.Equal(t, resourceURI, events[0][attr.ResourceURIKey])
	require.Empty(t, events[0][attr.ToolNameKey])
	require.Empty(t, events[0]["gram.mcp.risk.scan.prompt_name"])
	require.Equal(t, mcpriskscan.MethodResourcesRead, events[0]["gram.mcp.risk.scan.method"])
	require.Equal(t, mcpriskscan.PhaseRequest, events[0]["gram.mcp.risk.scan.phase"])
}
