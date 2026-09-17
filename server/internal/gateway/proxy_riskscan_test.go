package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type captureRiskScan struct {
	t        *testing.T
	events   []mcpriskscan.Event
	payloads [][]byte
}

func (s *captureRiskScan) Scan(_ context.Context, input io.Reader, event mcpriskscan.Event) {
	s.t.Helper()
	var payload []byte
	if input != nil {
		var err error
		payload, err = io.ReadAll(input)
		require.NoError(s.t, err)
	}
	s.events = append(s.events, event)
	s.payloads = append(s.payloads, payload)
}

func TestToolProxy_RiskScanHTTPPreservesUpstreamResponse(t *testing.T) {
	t.Parallel()

	var received []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"error":"invalid selection"}`)
	}))
	t.Cleanup(server.Close)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	scan := &captureRiskScan{t: t, events: nil, payloads: nil}
	proxy := NewToolProxy(testenv.NewLogger(t), tracerProvider, testenv.NewMeterProvider(t), ToolCallSourceMCP, testenv.NewEncryptionClient(t), nil, policy, nil, nil, scan)
	descriptor := newTestToolDescriptor()
	plan := NewHTTPToolCallPlan(descriptor, &HTTPToolCallPlan{
		ServerEnvVar: "", DefaultServerUrl: NullString{Value: server.URL, Valid: true},
		Security: nil, SecurityScopes: nil, Method: http.MethodPost, Path: "/selection", Schema: nil,
		HeaderParams: nil, QueryParams: nil, PathParams: nil,
		RequestContentType: NullString{Value: "application/json", Valid: true}, ResponseFilter: nil,
	})
	body := json.RawMessage(`{"body":{"selection":"unchanged"}}`)
	route := CallRoute{Source: ToolCallSourceMCP, ServerID: uuid.NewString(), ToolsetID: uuid.NewString(), Payload: body}
	recorder := httptest.NewRecorder()
	err = proxy.Do(t.Context(), recorder, iotest.OneByteReader(bytes.NewReader(body)), toolconfig.ToolCallEnv{
		SystemEnv: toolconfig.NewCaseInsensitiveEnv(), UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "", GramEmail: "", GramChatID: "", MCPClient: toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""},
	}, plan, tm.HTTPLogAttributes{}, route)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	require.JSONEq(t, `{"error":"invalid selection"}`, recorder.Body.String())
	require.JSONEq(t, `{"selection":"unchanged"}`, string(received))
	require.Equal(t, []mcpriskscan.Event{{
		Surface: mcpriskscan.SurfaceHostedMCP, Method: mcpriskscan.MethodToolsCall, OrganizationID: descriptor.OrganizationID, ProjectID: descriptor.ProjectID,
		ServerID: route.ServerID, ToolsetID: route.ToolsetID, ToolName: descriptor.Name,
		ResourceURI: "", PromptName: "", Phase: mcpriskscan.PhaseBeforeExecution,
	}}, scan.events)
	require.Equal(t, [][]byte{[]byte(`{"body":{"selection":"unchanged"}}`)}, scan.payloads)
}

func TestToolProxy_RiskScanExternalMCPPreservesResult(t *testing.T) {
	t.Parallel()

	server := newExternalMCPTestServer(t, false)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	scan := &captureRiskScan{t: t, events: nil, payloads: nil}
	proxy := NewToolProxy(testenv.NewLogger(t), tracerProvider, testenv.NewMeterProvider(t), ToolCallSourceMCP, testenv.NewEncryptionClient(t), nil, policy, nil, nil, scan)
	descriptor := newExternalMCPToolDescriptor()
	descriptor.Name = "proxy_placeholder"
	recorder, err := callToolProxy(t, t.Context(), proxy, NewExternalMCPToolCallPlan(descriptor, externalMCPPlan(server.URL)), `{}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"content":[{"type":"text","text":"result"}]}`, recorder.Body.String())
	require.Equal(t, []mcpriskscan.Event{{
		Surface: mcpriskscan.SurfaceHostedMCP, Method: mcpriskscan.MethodToolsCall, OrganizationID: descriptor.OrganizationID, ProjectID: descriptor.ProjectID,
		ServerID: "", ToolsetID: "", ToolName: descriptor.URN.Name,
		ResourceURI: "", PromptName: "", Phase: mcpriskscan.PhaseBeforeExecution,
	}}, scan.events)
	require.Equal(t, [][]byte{[]byte(`{}`)}, scan.payloads)
}

func TestToolProxy_RiskScanExternalMCPPreservesToolError(t *testing.T) {
	t.Parallel()

	server := newExternalMCPTestServer(t, true)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	scan := &captureRiskScan{t: t, events: nil, payloads: nil}
	proxy := NewToolProxy(testenv.NewLogger(t), tracerProvider, testenv.NewMeterProvider(t), ToolCallSourceMCP, testenv.NewEncryptionClient(t), nil, policy, nil, nil, scan)
	descriptor := newExternalMCPToolDescriptor()
	descriptor.Name = "proxy_placeholder"
	recorder, err := callToolProxy(t, t.Context(), proxy, NewExternalMCPToolCallPlan(descriptor, externalMCPPlan(server.URL)), `{}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"content":[{"type":"text","text":"result"}],"isError":true}`, recorder.Body.String())
	require.Equal(t, []mcpriskscan.Event{{
		Surface: mcpriskscan.SurfaceHostedMCP, Method: mcpriskscan.MethodToolsCall, OrganizationID: descriptor.OrganizationID, ProjectID: descriptor.ProjectID,
		ServerID: "", ToolsetID: "", ToolName: descriptor.URN.Name,
		ResourceURI: "", PromptName: "", Phase: mcpriskscan.PhaseBeforeExecution,
	}}, scan.events)
	require.Equal(t, [][]byte{[]byte(`{}`)}, scan.payloads)
}

func TestToolProxy_RiskScanPromptPreservesRendering(t *testing.T) {
	t.Parallel()

	tracerProvider := testenv.NewTracerProvider(t)
	scan := &captureRiskScan{t: t, events: nil, payloads: nil}
	proxy := NewToolProxy(testenv.NewLogger(t), tracerProvider, testenv.NewMeterProvider(t), ToolCallSourceMCP, nil, nil, nil, nil, nil, scan)
	plan := newPromptToolCallPlanForTest("mustache")
	recorder, err := callToolProxy(t, t.Context(), proxy, plan, `{"arguments":{"topic":"sample"}}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "Summarize sample", recorder.Body.String())
	require.Equal(t, []mcpriskscan.Event{{
		Surface: mcpriskscan.SurfaceHostedMCP, Method: mcpriskscan.MethodToolsCall, OrganizationID: plan.Descriptor.OrganizationID, ProjectID: plan.Descriptor.ProjectID,
		ServerID: "", ToolsetID: "", ToolName: plan.Descriptor.Name,
		ResourceURI: "", PromptName: "", Phase: mcpriskscan.PhaseBeforeExecution,
	}}, scan.events)
	require.Equal(t, [][]byte{[]byte(`{"arguments":{"topic":"sample"}}`)}, scan.payloads)
}

func TestToolProxy_RiskScanAllowsStreamWithoutPayload(t *testing.T) {
	t.Parallel()

	tracerProvider := testenv.NewTracerProvider(t)
	scan := &captureRiskScan{t: t, events: nil, payloads: nil}
	proxy := NewToolProxy(testenv.NewLogger(t), tracerProvider, testenv.NewMeterProvider(t), ToolCallSourceMCP, nil, nil, nil, nil, nil, scan)
	plan := newPromptToolCallPlanForTest("mustache")
	body := iotest.OneByteReader(strings.NewReader(`{"arguments":{"topic":"streamed"}}`))
	recorder := httptest.NewRecorder()
	err := proxy.Do(t.Context(), recorder, body, toolconfig.ToolCallEnv{
		SystemEnv: toolconfig.NewCaseInsensitiveEnv(), UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "", GramEmail: "", GramChatID: "", MCPClient: toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""},
	}, plan, tm.HTTPLogAttributes{}, CallRoute{Source: ToolCallSourceMCP, ServerID: "", ToolsetID: "", Payload: nil})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "Summarize streamed", recorder.Body.String())
	require.Equal(t, [][]byte{nil}, scan.payloads, "stream-only callers must not materialize a scan payload")
}
