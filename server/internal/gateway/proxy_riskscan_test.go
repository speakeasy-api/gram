package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/riskscan"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type captureRiskScan struct {
	events []riskscan.Event
}

func (s *captureRiskScan) Scan(_ context.Context, event riskscan.Event) {
	s.events = append(s.events, event)
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
	scan := &captureRiskScan{events: nil}
	proxy := NewToolProxy(testenv.NewLogger(t), tracerProvider, testenv.NewMeterProvider(t), ToolCallSourceMCP, testenv.NewEncryptionClient(t), nil, policy, nil, nil, scan)
	descriptor := newTestToolDescriptor()
	plan := NewHTTPToolCallPlan(descriptor, &HTTPToolCallPlan{
		ServerEnvVar: "", DefaultServerUrl: NullString{Value: server.URL, Valid: true},
		Security: nil, SecurityScopes: nil, Method: http.MethodPost, Path: "/selection", Schema: nil,
		HeaderParams: nil, QueryParams: nil, PathParams: nil,
		RequestContentType: NullString{Value: "application/json", Valid: true}, ResponseFilter: nil,
	})
	target := riskscan.Target{Surface: riskscan.SurfaceHostedMCP, ServerID: uuid.NewString(), ToolsetID: uuid.NewString()}
	recorder := httptest.NewRecorder()
	err = proxy.Do(t.Context(), recorder, bytes.NewBufferString(`{"body":{"selection":"unchanged"}}`), toolconfig.ToolCallEnv{
		SystemEnv: toolconfig.NewCaseInsensitiveEnv(), UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "", GramEmail: "", GramChatID: "", MCPClient: toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""},
	}, plan, tm.HTTPLogAttributes{}, target)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	require.JSONEq(t, `{"error":"invalid selection"}`, recorder.Body.String())
	require.JSONEq(t, `{"selection":"unchanged"}`, string(received))
	require.Equal(t, []riskscan.Event{{
		Surface: target.Surface, OrganizationID: descriptor.OrganizationID, ProjectID: descriptor.ProjectID,
		ServerID: target.ServerID, ToolsetID: target.ToolsetID, ToolName: descriptor.Name,
		ResourceURI: "", PromptName: "", Phase: riskscan.PhaseBeforeExecution,
	}}, scan.events)
}

func TestToolProxy_RiskScanExternalMCPPreservesResult(t *testing.T) {
	t.Parallel()

	server := newExternalMCPTestServer(t, false)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	scan := &captureRiskScan{events: nil}
	proxy := NewToolProxy(testenv.NewLogger(t), tracerProvider, testenv.NewMeterProvider(t), ToolCallSourceMCP, testenv.NewEncryptionClient(t), nil, policy, nil, nil, scan)
	descriptor := newExternalMCPToolDescriptor()
	descriptor.Name = "proxy_placeholder"
	recorder, err := callToolProxy(t, t.Context(), proxy, NewExternalMCPToolCallPlan(descriptor, externalMCPPlan(server.URL)), `{}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"content":[{"type":"text","text":"result"}]}`, recorder.Body.String())
	require.Equal(t, []riskscan.Event{{
		Surface: riskscan.SurfaceHostedMCP, OrganizationID: descriptor.OrganizationID, ProjectID: descriptor.ProjectID,
		ServerID: "", ToolsetID: "", ToolName: "search",
		ResourceURI: "", PromptName: "", Phase: riskscan.PhaseBeforeExecution,
	}}, scan.events)
}

func TestToolProxy_RiskScanExternalMCPPreservesToolError(t *testing.T) {
	t.Parallel()

	server := newExternalMCPTestServer(t, true)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	scan := &captureRiskScan{events: nil}
	proxy := NewToolProxy(testenv.NewLogger(t), tracerProvider, testenv.NewMeterProvider(t), ToolCallSourceMCP, testenv.NewEncryptionClient(t), nil, policy, nil, nil, scan)
	descriptor := newExternalMCPToolDescriptor()
	descriptor.Name = "proxy_placeholder"
	recorder, err := callToolProxy(t, t.Context(), proxy, NewExternalMCPToolCallPlan(descriptor, externalMCPPlan(server.URL)), `{}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"content":[{"type":"text","text":"result"}],"isError":true}`, recorder.Body.String())
	require.Equal(t, []riskscan.Event{{
		Surface: riskscan.SurfaceHostedMCP, OrganizationID: descriptor.OrganizationID, ProjectID: descriptor.ProjectID,
		ServerID: "", ToolsetID: "", ToolName: "search",
		ResourceURI: "", PromptName: "", Phase: riskscan.PhaseBeforeExecution,
	}}, scan.events)
}

func TestToolProxy_RiskScanPromptPreservesRendering(t *testing.T) {
	t.Parallel()

	tracerProvider := testenv.NewTracerProvider(t)
	scan := &captureRiskScan{events: nil}
	proxy := NewToolProxy(testenv.NewLogger(t), tracerProvider, testenv.NewMeterProvider(t), ToolCallSourceMCP, nil, nil, nil, nil, nil, scan)
	plan := newPromptToolCallPlanForTest("mustache")
	recorder, err := callToolProxy(t, t.Context(), proxy, plan, `{"arguments":{"topic":"sample"}}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "Summarize sample", recorder.Body.String())
	require.Equal(t, []riskscan.Event{{
		Surface: riskscan.SurfaceHostedMCP, OrganizationID: plan.Descriptor.OrganizationID, ProjectID: plan.Descriptor.ProjectID,
		ServerID: "", ToolsetID: "", ToolName: plan.Descriptor.Name,
		ResourceURI: "", PromptName: "", Phase: riskscan.PhaseBeforeExecution,
	}}, scan.events)
}
