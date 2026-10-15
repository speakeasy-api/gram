package platformmcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCatalogProbeFailureDistinguishesResponseSizeByStage(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		stage        catalogProbeStage
		wantState    ReadinessState
		wantEvidence string
	}{
		{name: "initialize", stage: catalogProbeStageInitialize, wantState: ReadinessUnsupported, wantEvidence: "initialize_response_too_large"},
		{name: "tools list", stage: catalogProbeStageToolsList, wantState: ReadinessDegraded, wantEvidence: "tools_list_response_too_large"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			roundTripper := &catalogAuthorizationRoundTripper{}
			roundTripper.tooLarge.Store(true)
			state, evidence := catalogProbeFailure(errCatalogProbeResponseTooLarge, roundTripper, test.stage)
			require.Equal(t, test.wantState, state)
			require.Equal(t, test.wantEvidence, evidence)
		})
	}
}

func TestCatalogProbeFailureKeepsTransientPrecedenceOverResponseSize(t *testing.T) {
	t.Parallel()

	roundTripper := &catalogAuthorizationRoundTripper{}
	roundTripper.transient.Store(true)
	roundTripper.tooLarge.Store(true)
	state, evidence := catalogProbeFailure(errCatalogProbeResponseTooLarge, roundTripper, catalogProbeStageInitialize)
	require.Equal(t, ReadinessDegraded, state)
	require.Equal(t, "probe_temporarily_unavailable", evidence)
}

func TestTransientCatalogProbeStatusIncludesRequestTimeout(t *testing.T) {
	t.Parallel()

	require.True(t, transientCatalogProbeStatus(http.StatusRequestTimeout))
	require.False(t, transientCatalogProbeStatus(http.StatusBadRequest))
}

func TestCatalogProbeFailurePrecedenceAndRedirect(t *testing.T) {
	t.Parallel()

	unauthorized := &catalogAuthorizationRoundTripper{}
	unauthorized.unauthorized.Store(true)
	unauthorized.transient.Store(true)
	state, evidence := catalogProbeFailure(errors.New("private HTTP detail"), unauthorized, catalogProbeStageInitialize)
	require.Equal(t, ReadinessUnauthorized, state)
	require.Equal(t, "upstream_authorization_rejected", evidence)

	redirected := &catalogAuthorizationRoundTripper{}
	redirected.responded.Store(true)
	redirected.redirected.Store(true)
	state, evidence = catalogProbeFailure(errors.New("private HTTP detail"), redirected, catalogProbeStageInitialize)
	require.Equal(t, ReadinessUnsupported, state)
	require.Equal(t, "redirect_rejected", evidence)
}

func TestCatalogProbeFailureClassifiesHTTPProtocolAndTransportFailures(t *testing.T) {
	t.Parallel()

	response := &catalogAuthorizationRoundTripper{}
	response.responded.Store(true)
	state, evidence := catalogProbeFailure(errors.New("private SDK detail"), response, catalogProbeStageInitialize)
	require.Equal(t, ReadinessUnsupported, state)
	require.Equal(t, "invalid_mcp_response", evidence)

	state, evidence = catalogProbeFailure(errors.New("private transport detail"), &catalogAuthorizationRoundTripper{}, catalogProbeStageInitialize)
	require.Equal(t, ReadinessUnreachable, state)
	require.Equal(t, "probe_failed", evidence)

	transient := &catalogAuthorizationRoundTripper{}
	transient.responded.Store(true)
	transient.transient.Store(true)
	state, evidence = catalogProbeFailure(errors.New("private HTTP detail"), transient, catalogProbeStageInitialize)
	require.Equal(t, ReadinessDegraded, state)
	require.Equal(t, "probe_temporarily_unavailable", evidence)
}

func TestRemoteMCPReadinessProbeLogsRejectedToolsList(t *testing.T) {
	t.Parallel()

	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "probe-target", Version: "1.0.0"}, nil)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read request body", http.StatusInternalServerError)
			return
		}
		if bytes.Contains(body, []byte(`"tools/list"`)) {
			http.Error(w, "tools listing rejected", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		mcpHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	var logs bytes.Buffer
	prober := &RemoteMCPReadinessProber{logger: slog.New(slog.NewJSONHandler(&logs, nil)), policy: policy}

	state, evidence := prober.probe(t.Context(), upstream.URL+"?tenant=probe-query-sentinel", nil, "probe-token-sentinel")

	require.Equal(t, ReadinessUnsupported, state)
	require.Equal(t, "invalid_mcp_response", evidence)
	require.Contains(t, logs.String(), "remote mcp readiness probe failed")
	require.Contains(t, logs.String(), `"tools_list"`)
	require.Contains(t, logs.String(), "400")
	require.NotContains(t, logs.String(), "probe-token-sentinel")
	require.NotContains(t, logs.String(), "probe-query-sentinel")
}

func TestRedactProbeErrorRemovesCredentialsAndBoundsLength(t *testing.T) {
	t.Parallel()

	roundTripper := &catalogAuthorizationRoundTripper{token: "token-sentinel"}
	detail := redactProbeError(`Post "https://mcp.example.test/mcp?key=query-sentinel": token-sentinel `+strings.Repeat("x", catalogProbeMaxLoggedError), "https://mcp.example.test/mcp?key=query-sentinel", roundTripper)

	require.NotContains(t, detail, "token-sentinel")
	require.NotContains(t, detail, "query-sentinel")
	require.Contains(t, detail, "https://mcp.example.test/mcp?[REDACTED]")
	require.LessOrEqual(t, len(detail), catalogProbeMaxLoggedError+len("…"))
}

// TestRemoteMCPReadinessProbeIgnoresInboundProtocolVersion runs the probe from
// inside an MCP tool call, as get_mcp_readiness does in production. The inbound
// request negotiates 2026-07-28; the upstream only speaks legacy revisions and
// rejects a 2026-07-28 header after initialize, as the reported provider did.
func TestRemoteMCPReadinessProbeIgnoresInboundProtocolVersion(t *testing.T) {
	t.Parallel()

	legacyServer := mcp.NewServer(&mcp.Implementation{Name: "legacy-upstream", Version: "1.0.0"}, nil)
	mcp.AddTool(legacyServer, &mcp.Tool{Name: "noop"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
		return &mcp.CallToolResult{}, struct{}{}, nil
	})
	legacyHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return legacyServer }, nil)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read request body", http.StatusInternalServerError)
			return
		}
		initialize := bytes.Contains(body, []byte(`"method":"initialize"`))
		if !initialize && r.Header.Get("Mcp-Protocol-Version") == "2026-07-28" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Bad Request: Unsupported protocol version: 2026-07-28"},"id":null}`))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		legacyHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	prober := &RemoteMCPReadinessProber{logger: testenv.NewLogger(t), policy: policy}

	platformServer := mcp.NewServer(&mcp.Implementation{Name: "platform", Version: "1.0.0"}, nil)
	mcp.AddTool(platformServer, &mcp.Tool{Name: "probe"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
		state, evidence := prober.probe(ctx, upstream.URL, nil, "")
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(state) + " " + evidence}}}, struct{}{}, nil
	})
	// The Platform MCP runs stateless, which dispatches tool handlers on the
	// inbound HTTP request context.
	platform := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return platformServer }, &mcp.StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(platform.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "caller", Version: "1.0.0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: platform.URL, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "probe"})
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Equal(t, "ready tools_list_ok", text.Text)
}
