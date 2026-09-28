package platformmcp

import (
	"bytes"
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
