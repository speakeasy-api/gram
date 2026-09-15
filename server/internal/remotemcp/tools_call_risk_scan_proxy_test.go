package remotemcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const riskScanServerID = "0198a0b0-0000-7000-8000-000000000001"

const riskScanRequest = " {\n  \"jsonrpc\": \"2.0\", \"id\": 7, \"method\": \"tools/call\", \"params\": {\"name\": \"lookup\", \"arguments\": {\"query\": \"sample\"}, \"_meta\": {\"progressToken\": \"p\"}}\n}\n"

type recordingRemoteRiskScan struct {
	events []mcpriskscan.Event
	calls  atomic.Int32
}

func (r *recordingRemoteRiskScan) Scan(_ context.Context, event mcpriskscan.Event) {
	r.events = append(r.events, event)
	r.calls.Add(1)
}

func TestToolsCallRiskScanNoopDoesNotRejectUnclassifiedCall(t *testing.T) {
	t.Parallel()
	interceptor := &toolsCallRiskScanInterceptor{
		hook: mcpriskscan.NewNoop(testenv.NewTracerProvider(t)),
		event: mcpriskscan.Event{
			Surface: mcpriskscan.SurfaceRemoteMCP, OrganizationID: "", ProjectID: "", ServerID: "",
			ToolsetID: "", ToolName: "", ResourceURI: "", PromptName: "", Phase: mcpriskscan.PhaseBeforeExecution,
			Payload: nil,
		},
	}
	// The request phase decoded Params, but there is no known tool schema,
	// target identity, or stamped principal for the hook to classify.
	call := &proxy.ToolsCallRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "unclassified-operation",
			Arguments: json.RawMessage(`{"unrecognized_shape":[true,7]}`),
			Meta:      nil,
		},
		UserRequest: nil,
	}
	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call),
		"an observation-only hook must never reject an unclassifiable call")
	require.Equal(t, "unclassified-operation", call.Params.Name)
	require.JSONEq(t, `{"unrecognized_shape":[true,7]}`, string(call.Params.Arguments))
}

func TestProxyManagerRiskScanPreservesJSONExchange(t *testing.T) {
	t.Parallel()
	assertRiskScanRelay(t, false, riskScanRequest, http.StatusOK, "application/json",
		" {\n\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"done\"}]}}\n", true)
}

func TestProxyManagerRiskScanPreservesTunnelSSEExchange(t *testing.T) {
	t.Parallel()
	assertRiskScanRelay(t, true, riskScanRequest, http.StatusOK, "text/event-stream",
		": keepalive\n\nevent: message\nid: progress\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progressToken\":\"p\",\"progress\":0.5}}\n\n"+
			"event: message\nid: unrelated\ndata: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{\"content\":[]}}\n\n"+
			"event: message\nid: terminal\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"done\"}]}}\n\n", true)
}

func TestProxyManagerRiskScanPreservesUpstreamRejection(t *testing.T) {
	t.Parallel()
	assertRiskScanRelay(t, false, riskScanRequest, http.StatusForbidden, "application/json",
		`{"jsonrpc":"2.0","id":7,"error":{"code":-32000,"message":"upstream rejected"}}`, true)
}

func TestProxyManagerRiskScanPreservesUnreadableUpstreamResponse(t *testing.T) {
	t.Parallel()
	assertRiskScanRelay(t, false, riskScanRequest, http.StatusBadGateway, "application/json", "not-json\n", true)
}

func TestProxyManagerRiskScanSkipsMalformedPublicCall(t *testing.T) {
	t.Parallel()
	assertRiskScanRelay(t, false, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":42}}`,
		http.StatusBadRequest, "application/json",
		`{"jsonrpc":"2.0","id":7,"error":{"code":-32602,"message":"invalid tool name"}}`, false)
}

func TestProxyManagerRiskScanRunsAfterSessionSelection(t *testing.T) {
	t.Parallel()
	assertRiskScanSelectionRejection(t, riskScanRequest, proxy.RejectCodeInvalidRequest)
}

func TestProxyManagerRiskScanSkipsMalformedStrictCall(t *testing.T) {
	t.Parallel()
	assertRiskScanSelectionRejection(t, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":42}}`, proxy.RejectCodeInvalidParams)
}

func assertRiskScanRelay(t *testing.T, tunnel bool, request string, status int, contentType, response string, scanned bool) {
	t.Helper()
	recorded := &recordingRemoteRiskScan{events: nil, calls: atomic.Int32{}}
	type forwardedRequest struct {
		body      string
		scanCalls int32
		err       error
	}
	forwarded := make(chan forwardedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		forwarded <- forwardedRequest{body: string(body), scanCalls: recorded.calls.Load(), err: err}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Upstream-Result", "preserved")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(upstream.Close)

	built := newRiskScanTestProxy(t, upstream.URL, recorded, nil, tunnel)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/sample", strings.NewReader(request))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()
	require.NoError(t, built.Post(rr, req))
	require.Equal(t, status, rr.Code)
	require.Equal(t, response, rr.Body.String())
	require.Equal(t, contentType, rr.Header().Get("Content-Type"))
	require.Equal(t, "preserved", rr.Header().Get("X-Upstream-Result"))

	select {
	case received := <-forwarded:
		require.NoError(t, received.err)
		require.Equal(t, request, received.body)
		if scanned {
			require.Equal(t, int32(1), received.scanCalls, "scan must happen before upstream execution")
		} else {
			require.Zero(t, received.scanCalls)
		}
	default:
		t.Fatal("request did not reach upstream")
	}
	if scanned {
		require.Equal(t, []mcpriskscan.Event{{
			Surface:        mcpriskscan.SurfaceRemoteMCP,
			OrganizationID: "org-test",
			ProjectID:      "project-test",
			ServerID:       riskScanServerID,
			ToolsetID:      "",
			ToolName:       "lookup",
			ResourceURI:    "",
			PromptName:     "",
			Phase:          mcpriskscan.PhaseBeforeExecution,
			Payload:        json.RawMessage(`{"query": "sample"}`),
		}}, recorded.events)
	} else {
		require.Empty(t, recorded.events)
	}
}

func assertRiskScanSelectionRejection(t *testing.T, request string, code int64) {
	t.Helper()
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamCalls.Add(1)
	}))
	t.Cleanup(upstream.Close)
	selection, err := toolfilter.ParseSessionSelection([]byte(`{"resource":"mcp_server:` + riskScanServerID + `","grant_id":"0198a0b0-0000-7000-8000-0000000000aa","allow":[{"type":"tool","name":"other"}]}`))
	require.NoError(t, err)
	recorded := &recordingRemoteRiskScan{events: nil, calls: atomic.Int32{}}
	built := newRiskScanTestProxy(t, upstream.URL, recorded, selection, false)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/sample", strings.NewReader(request))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	require.NoError(t, built.Post(rr, req))
	require.Equal(t, http.StatusOK, rr.Code)
	var envelope struct {
		ID    int `json:"id"`
		Error struct {
			Code int64 `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &envelope))
	require.Equal(t, 7, envelope.ID)
	require.Equal(t, code, envelope.Error.Code)
	require.Zero(t, upstreamCalls.Load())
	require.Empty(t, recorded.events)
}

func newRiskScanTestProxy(t *testing.T, upstreamURL string, hook mcpriskscan.Hook, selection *toolfilter.SessionSelection, tunnel bool) *proxy.Proxy {
	t.Helper()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	manager := NewProxyManager(logger, tracerProvider, testenv.NewMeterProvider(t),
		nil, policy, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	manager.riskScan = hook
	if tunnel {
		return manager.BuildTarget(logger, proxy.ServerIdentity{
			RemoteMCPServerID:   "",
			TunneledMCPServerID: uuid.NewString(),
			McpServerID:         riskScanServerID,
			MetaMCPServerID:     "",
		}, upstreamURL, nil, mcpservers.VisibilityPublic, "org-test", "project-test", "", "", selection)
	}
	return manager.Build(logger, &remotemcprepo.RemoteMcpServer{ID: uuid.New(), Url: upstreamURL},
		riskScanServerID, nil, mcpservers.VisibilityPublic, "org-test", "project-test", "", "", selection)
}
