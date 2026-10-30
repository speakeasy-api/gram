package platformmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestSetupProbeInitializesAndListsWithoutCallingTools(t *testing.T) {
	t.Parallel()
	var methods sync.Map
	server := mcp.NewServer(&mcp.Implementation{Name: "setup-test", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "example", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		t.Error("tool was executed")
		return nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer setup-token" {
			t.Error("probe sent wrong authorization")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read MCP request: %v", err)
			http.Error(w, "read request", http.StatusBadRequest)
			return
		}
		var message struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &message)
		if message.Method != "" {
			methods.Store(message.Method, true)
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	prober := &RemoteMCPReadinessProber{logger: testenv.NewLogger(t), policy: policy}
	state, evidence, count := prober.probeTools(t.Context(), upstream.URL, nil, "setup-token")
	require.Equal(t, ReadinessReady, state)
	require.Equal(t, "tools_list_ok", evidence)
	require.Equal(t, 1, count)
	_, initialized := methods.Load("initialize")
	require.True(t, initialized)
	_, listed := methods.Load("tools/list")
	require.True(t, listed)
	_, called := methods.Load("tools/call")
	require.False(t, called)
}

func TestSetupProbeRequiresEveryToolPage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, secondPage string
		status           int
		wantState        ReadinessState
		wantCode         string
		wantCount        int
	}{
		{"all pages", `{"tools":[{"name":"second","inputSchema":{"type":"object"}}]}`, http.StatusOK, ReadinessReady, "tools_list_ok", 2},
		{"later authorization failure", "private provider detail", http.StatusUnauthorized, ReadinessUnauthorized, "upstream_authorization_rejected", 0},
		{"repeated cursor", `{"tools":[],"nextCursor":"again"}`, http.StatusOK, ReadinessDegraded, "tools_list_cursor_repeated", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := mcp.NewServer(&mcp.Implementation{Name: "pages", Version: "1"}, nil)
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("read MCP request: %v", err)
					http.Error(w, "read request", http.StatusBadRequest)
					return
				}
				var message struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
					Params struct {
						Cursor string `json:"cursor"`
					} `json:"params"`
				}
				if len(body) > 0 {
					if err := json.Unmarshal(body, &message); err != nil {
						t.Errorf("decode MCP request: %v", err)
						http.Error(w, "bad request", http.StatusBadRequest)
						return
					}
				}
				if message.Method == "tools/list" {
					result := `{"tools":[{"name":"first","inputSchema":{"type":"object"}}],"nextCursor":"again"}`
					if message.Params.Cursor != "" {
						if tc.status != http.StatusOK {
							http.Error(w, tc.secondPage, tc.status)
							return
						}
						result = tc.secondPage
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, message.ID, result)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(upstream.Close)
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)
			prober := &RemoteMCPReadinessProber{logger: testenv.NewLogger(t), policy: policy}
			state, code, count := prober.probeTools(t.Context(), upstream.URL, nil, "page-token")
			require.Equal(t, tc.wantState, state)
			require.Equal(t, tc.wantCode, code)
			require.Equal(t, tc.wantCount, count)
		})
	}
}
