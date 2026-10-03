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
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	riskScanServerID  = "0198a0b0-0000-7000-8000-000000000001"
	riskScanProjectID = "0198a0b0-0000-7000-8000-000000000002"
)

const riskScanRequest = " {\n  \"jsonrpc\": \"2.0\", \"id\": 7, \"method\": \"tools/call\", \"params\": {\"name\": \"lookup\", \"arguments\": {\"query\": \"sample\"}, \"_meta\": {\"progressToken\": \"p\"}}\n}\n"

type recordingRemoteRiskScan struct {
	events   []mcpriskscan.Event
	payloads [][]byte
	calls    atomic.Int32
}

func (r *recordingRemoteRiskScan) Observe(_ context.Context, subject mcpriskscan.Subject) {
	r.payloads = append(r.payloads, append([]byte(nil), subject.Payload.Bytes()...))
	r.events = append(r.events, subject.Event)
	r.calls.Add(1)
}

type remotePolicyLookup struct {
	policy policycore.Policy
}

func (l remotePolicyLookup) ListEnabledForMCP(context.Context, string, uuid.UUID, policycore.MCPTarget) ([]policycore.Policy, error) {
	return []policycore.Policy{l.policy}, nil
}

type remotePolicyDetector struct{}

func (remotePolicyDetector) ScanMCPPolicy(context.Context, policycore.Policy, risk.MCPScanRequest) ([]scanners.Finding, error) {
	return []scanners.Finding{{RuleID: "remote.block", Description: "Blocked remote input", Tags: []string{}, Source: "gitleaks", Confidence: 1}}, nil
}

type remoteResponsePolicyDetector struct{}

func (remoteResponsePolicyDetector) ScanMCPPolicy(_ context.Context, _ policycore.Policy, request risk.MCPScanRequest) ([]scanners.Finding, error) {
	if request.MessageType != message.ToolResponse {
		return nil, nil
	}
	return []scanners.Finding{{
		RuleID: "remote.response", Description: "Blocked remote output", Match: "person@example.com",
		StartPos: 0, EndPos: 18, Tags: []string{}, Source: "presidio", Confidence: 1,
	}}, nil
}

type findingChannel chan *riskv1.Finding

func (p findingChannel) Publish(_ context.Context, finding *riskv1.Finding, _ ...gcp.PublishOption) gcp.PublishResult {
	p <- finding
	return gcp.NewSuccessPublishResult()
}

func (findingChannel) Stop(context.Context) error { return nil }

func TestProxyManagerRiskScanBlocksBeforeUpstream(t *testing.T) {
	t.Parallel()
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamCalls.Add(1)
	}))
	t.Cleanup(upstream.Close)
	projectID := uuid.MustParse(riskScanProjectID)
	evaluator := mcpriskscan.NewPolicyEvaluator(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		remotePolicyLookup{policy: policycore.Policy{
			ID:             uuid.New(),
			ProjectID:      projectID,
			OrganizationID: "org-test",
			Name:           "Remote policy",
			Action:         "block",
		}},
		remotePolicyDetector{},
		gcp.NewNoopPublisher[*riskv1.Finding](),
		newMCPFindingEvidenceForTest(t),
		mcpriskscan.DefaultPolicyConfig,
	)
	built := newRiskScanTestProxyWithEvaluator(t, upstream.URL, evaluator, nil, false)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/sample", strings.NewReader(riskScanRequest))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()

	require.NoError(t, built.Post(rr, req))
	require.Equal(t, http.StatusOK, rr.Code)
	var envelope struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &envelope))
	require.Equal(t, proxy.RejectCodeForbidden, envelope.Error.Code)
	require.Contains(t, envelope.Error.Message, "Remote policy")
	require.Zero(t, upstreamCalls.Load())
}

func TestProxyManagerRiskScanFlagPublishesAndAllowsUpstream(t *testing.T) {
	t.Parallel()
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"person@example.com"}]}}`)
	}))
	t.Cleanup(upstream.Close)
	projectID := uuid.MustParse(riskScanProjectID)
	published := make(findingChannel, 1)
	evaluator := mcpriskscan.NewPolicyEvaluator(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		remotePolicyLookup{policy: policycore.Policy{
			ID:             uuid.New(),
			ProjectID:      projectID,
			OrganizationID: "org-test",
			Name:           "Remote flag policy",
			Action:         "flag",
		}},
		remoteResponsePolicyDetector{},
		published,
		newMCPFindingEvidenceForTest(t),
		mcpriskscan.DefaultPolicyConfig,
	)
	built := newRiskScanTestProxyWithEvaluator(t, upstream.URL, evaluator, nil, false)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/sample", strings.NewReader(riskScanRequest))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()

	require.NoError(t, built.Post(rr, req))
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, int32(1), upstreamCalls.Load())
	require.Contains(t, rr.Body.String(), "person@example.com")
	select {
	case finding := <-published:
		require.Equal(t, riskv1.Finding_ENFORCEMENT_OUTCOME_LOGGED, finding.GetEnforcementOutcome())
		require.Equal(t, riskScanServerID, finding.GetExecution().GetMcpServerId())
		require.Equal(t, "lookup", finding.GetExecution().GetToolName())
		require.Equal(t, mcpriskscan.PhaseResponse, finding.GetExecution().GetPhase())
	case <-time.After(time.Second):
		t.Fatal("flag finding was not published")
	}
}

func TestProxyManagerRiskScanWithholdsRemoteResponse(t *testing.T) {
	t.Parallel()
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"person@example.com"}]}}`)
	}))
	t.Cleanup(upstream.Close)
	projectID := uuid.MustParse(riskScanProjectID)
	published := make(findingChannel, 1)
	userMessage := "Remove personal data before continuing."
	evaluator := mcpriskscan.NewPolicyEvaluator(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		remotePolicyLookup{policy: policycore.Policy{
			ID:             uuid.New(),
			ProjectID:      projectID,
			OrganizationID: "org-test",
			Name:           "Remote response policy",
			Action:         "block",
			UserMessage:    &userMessage,
		}},
		remoteResponsePolicyDetector{},
		published,
		newMCPFindingEvidenceForTest(t),
		mcpriskscan.DefaultPolicyConfig,
	)
	built := newRiskScanTestProxyWithEvaluator(t, upstream.URL, evaluator, nil, false)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x/mcp/sample", strings.NewReader(riskScanRequest))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()

	require.NoError(t, built.Post(rr, req))
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, int32(1), upstreamCalls.Load())
	var envelope struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &envelope))
	require.Equal(t, proxy.RejectCodeForbidden, envelope.Error.Code)
	require.Equal(t, userMessage, envelope.Error.Message)
	require.NotContains(t, rr.Body.String(), "person@example.com")
	select {
	case finding := <-published:
		require.Equal(t, riskv1.Finding_ENFORCEMENT_OUTCOME_WITHHELD, finding.GetEnforcementOutcome())
		require.Equal(t, mcpriskscan.PhaseResponse, finding.GetExecution().GetPhase())
	case <-time.After(time.Second):
		t.Fatal("withheld finding was not published")
	}
}

func TestToolsCallRiskScanFailsClosedWhenResultPayloadIsUnavailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message jsonrpc.Message
	}{
		{name: "unexpected message type", message: &jsonrpc.Request{Method: "notifications/progress"}},
		{name: "unparseable tool result", message: &jsonrpc.Response{Result: json.RawMessage(`{"content":[`)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			projectID := uuid.MustParse(riskScanProjectID)
			config := mcpriskscan.DefaultPolicyConfig
			config.FailMode = mcpriskscan.FailClosed
			evaluator := mcpriskscan.NewPolicyEvaluator(
				testenv.NewLogger(t),
				testenv.NewTracerProvider(t),
				testenv.NewMeterProvider(t),
				remotePolicyLookup{policy: policycore.Policy{
					ID:             uuid.New(),
					ProjectID:      projectID,
					OrganizationID: "org-test",
					Name:           "Remote response policy",
					Action:         "block",
				}},
				remoteResponsePolicyDetector{},
				gcp.NewNoopPublisher[*riskv1.Finding](),
				newMCPFindingEvidenceForTest(t),
				config,
			)
			interceptor := NewToolsCallRiskScanInterceptor(evaluator, mcpriskscan.Event{
				Surface: mcpriskscan.SurfaceRemoteMCP, Method: mcpriskscan.MethodToolsCall,
				OrganizationID: "org-test", ProjectID: projectID.String(), ServerID: riskScanServerID,
			})
			request := &proxy.ToolsCallRequest{
				Params: &mcp.CallToolParamsRaw{Name: "lookup", Arguments: json.RawMessage(`{}`)},
				UserRequest: &proxy.UserRequest{
					UserHTTPRequest: httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil),
				},
			}
			require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), request))

			err := interceptor.InterceptToolsCallResponse(t.Context(), &proxy.ToolsCallResponse{
				Request:       request,
				Result:        &mcp.CallToolResult{},
				RemoteMessage: &proxy.RemoteMessage{Message: test.message},
			})
			var rejection *proxy.RejectError
			require.ErrorAs(t, err, &rejection)
			require.Contains(t, rejection.Message, "evaluation did not complete")
		})
	}
}

func TestToolsCallRiskScanDoesNotRejectUnclassifiedCall(t *testing.T) {
	t.Parallel()
	interceptor := NewToolsCallRiskScanInterceptor(
		newRiskScanEvaluatorForTest(t),
		mcpriskscan.Event{
			Surface: mcpriskscan.SurfaceRemoteMCP, OrganizationID: "", ProjectID: "", ServerID: "",
			ToolsetID: "", ToolName: "", ResourceURI: "", PromptName: "",
			Method: mcpriskscan.MethodToolsCall,
		},
	)
	// The request phase decoded Params, but there is no known tool schema,
	// target identity, or stamped principal for the evaluator to classify.
	call := &proxy.ToolsCallRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "unclassified-operation",
			Arguments: json.RawMessage(`{"unrecognized_shape":[true,7]}`),
			Meta:      nil,
		},
		UserRequest: nil,
	}
	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call),
		"an observation-only scan must never reject an unclassifiable call")
	require.Equal(t, "unclassified-operation", call.Params.Name)
	require.JSONEq(t, `{"unrecognized_shape":[true,7]}`, string(call.Params.Arguments))
}

func TestProxyManagerRiskScanPreservesJSONExchange(t *testing.T) {
	t.Parallel()
	assertRiskScanRelay(t, false, riskScanRequest, http.StatusOK, "application/json",
		" {\n\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"done\"}]}}\n", true, "done")
}

func TestProxyManagerRiskScanPreservesTunnelSSEExchange(t *testing.T) {
	t.Parallel()
	assertRiskScanRelay(t, true, riskScanRequest, http.StatusOK, "text/event-stream",
		": keepalive\n\nevent: message\nid: progress\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progressToken\":\"p\",\"progress\":0.5}}\n\n"+
			"event: message\nid: unrelated\ndata: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{\"content\":[]}}\n\n"+
			"event: message\nid: terminal\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"done\"}]}}\n\n", true, "done")
}

func TestProxyManagerRiskScanPreservesUpstreamRejection(t *testing.T) {
	t.Parallel()
	assertRiskScanRelay(t, false, riskScanRequest, http.StatusForbidden, "application/json",
		`{"jsonrpc":"2.0","id":7,"error":{"code":-32000,"message":"upstream rejected"}}`, true, "upstream rejected")
}

func TestProxyManagerRiskScanPreservesUnreadableUpstreamResponse(t *testing.T) {
	t.Parallel()
	assertRiskScanRelay(t, false, riskScanRequest, http.StatusBadGateway, "application/json", "not-json\n", true, "")
}

func TestProxyManagerRiskScanSkipsMalformedPublicCall(t *testing.T) {
	t.Parallel()
	assertRiskScanRelay(t, false, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":42}}`,
		http.StatusBadRequest, "application/json",
		`{"jsonrpc":"2.0","id":7,"error":{"code":-32602,"message":"invalid tool name"}}`, false, "")
}

func TestProxyManagerRiskScanRunsAfterSessionSelection(t *testing.T) {
	t.Parallel()
	assertRiskScanSelectionRejection(t, riskScanRequest, proxy.RejectCodeInvalidRequest)
}

func TestProxyManagerRiskScanSkipsMalformedStrictCall(t *testing.T) {
	t.Parallel()
	assertRiskScanSelectionRejection(t, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":42}}`, proxy.RejectCodeInvalidParams)
}

func assertRiskScanRelay(t *testing.T, tunnel bool, request string, status int, contentType, response string, scanned bool, responsePayload string) {
	t.Helper()
	recorded := &recordingRemoteRiskScan{events: nil, payloads: nil, calls: atomic.Int32{}}
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
		wantPayloads := [][]byte{[]byte(`{"query": "sample"}`)}
		if responsePayload != "" {
			wantPayloads = append(wantPayloads, []byte(responsePayload))
		}
		require.Equal(t, wantPayloads, recorded.payloads)
		require.Len(t, recorded.events, len(wantPayloads))
		event := recorded.events[0]
		require.Equal(t, mcpriskscan.SurfaceRemoteMCP, event.Surface)
		require.Equal(t, mcpriskscan.MethodToolsCall, event.Method)
		require.Equal(t, "org-test", event.OrganizationID)
		require.Equal(t, riskScanProjectID, event.ProjectID)
		require.Equal(t, riskScanServerID, event.ServerID)
		require.Empty(t, event.MetaServerID)
		require.Empty(t, event.ToolsetID)
		require.Equal(t, "lookup", event.ToolName)
		require.Equal(t, mcpriskscan.PhaseRequest, event.Phase())
		require.NotEmpty(t, event.ExecutionID())
		if responsePayload != "" {
			require.Equal(t, mcpriskscan.PhaseResponse, recorded.events[1].Phase())
			require.Equal(t, event.ExecutionID(), recorded.events[1].ExecutionID())
		}
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
	recorded := &recordingRemoteRiskScan{events: nil, payloads: nil, calls: atomic.Int32{}}
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

func newRiskScanTestProxy(t *testing.T, upstreamURL string, observer mcpriskscan.Observer, selection *toolfilter.SessionSelection, tunnel bool) *proxy.Proxy {
	t.Helper()
	return newRiskScanTestProxyWithEvaluator(t, upstreamURL, mcpriskscan.PrependObserver(observer, newRiskScanEvaluatorForTest(t)), selection, tunnel)
}

func newRiskScanTestProxyWithEvaluator(t *testing.T, upstreamURL string, evaluator *mcpriskscan.Evaluator, selection *toolfilter.SessionSelection, tunnel bool) *proxy.Proxy {
	t.Helper()
	logger := testenv.NewLogger(t)
	manager := newProxyManagerForTest(t, newUnsafePolicyForTest(t), evaluator)
	if tunnel {
		return manager.BuildTarget(logger, proxy.ServerIdentity{
			RemoteMCPServerID:   "",
			TunneledMCPServerID: uuid.NewString(),
			McpServerID:         riskScanServerID,
			MetaMCPServerID:     "",
		}, upstreamURL, nil, mcpservers.VisibilityPublic, "org-test", riskScanProjectID, "", "", selection)
	}
	return manager.Build(logger, &remotemcprepo.RemoteMcpServer{ID: uuid.New(), Url: upstreamURL},
		riskScanServerID, nil, mcpservers.VisibilityPublic, "org-test", riskScanProjectID, "", "", selection)
}
