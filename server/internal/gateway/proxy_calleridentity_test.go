package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// callFunctionToolWithClient runs one function tool call and returns the
// `_meta` block the proxy handed to the runner along with the log attributes it
// recorded for the call.
func callFunctionToolWithClient(t *testing.T, client toolconfig.MCPClientIdentity) (*functions.ToolCallMeta, tm.HTTPLogAttributes) {
	t.Helper()

	var invocationID uuid.UUID
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result": "success"}`))
	}))
	t.Cleanup(mockServer.Close)

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	toolCallPlan := NewFunctionToolCallPlan(newTestToolDescriptor(), &FunctionToolCallPlan{
		FunctionID:        uuid.New().String(),
		FunctionsAccessID: uuid.New().String(),
		Runtime:           "nodejs",
		InputSchema:       []byte{},
		Variables:         map[string]*functions.ManifestVariableAttributeV0{},
		AuthInput:         nil,
	})

	var captured *functions.ToolCallMeta
	proxy := NewToolProxy(
		testenv.NewLogger(t),
		tracerProvider,
		testenv.NewMeterProvider(t),
		ToolCallSourceMCP,
		testenv.NewEncryptionClient(t),
		nil,
		policy,
		&mockToolCaller{
			serverURL: mockServer.URL,
			onCall:    func(invID uuid.UUID) { invocationID = invID },
			onRequest: func(req functions.RunnerToolCallRequest) { captured = req.Meta },
		},
		nil,
	)

	bodyBytes, err := json.Marshal(ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 json.RawMessage(`{"test": "data"}`),
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	})
	require.NoError(t, err)

	logAttrs := tm.HTTPLogAttributes{}
	err = proxy.Do(t.Context(), httptest.NewRecorder(), bytes.NewReader(bodyBytes), toolconfig.ToolCallEnv{
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: "",
		MCPClient:  client,
	}, toolCallPlan, logAttrs)
	require.NoError(t, err)

	return captured, logAttrs
}

func TestToolProxy_Do_FunctionTool_ForwardsCallerIdentity(t *testing.T) {
	t.Parallel()

	meta, _ := callFunctionToolWithClient(t, toolconfig.MCPClientIdentity{
		Name:          "claude-code",
		Version:       "2.1",
		OAuthClientID: "client-abc",
	})

	require.NotNil(t, meta)
	require.Equal(t, &functions.MCPClientInfo{Name: "claude-code", Version: "2.1"}, meta.ClientInfo)
	require.Equal(t, "client-abc", meta.OAuthClientID)
}

// TestToolProxy_Do_RecordsCallerIdentityOnLogAttributes pins the telemetry half
// of caller identity: every tool call funnels through the proxy, so the row
// written for the call carries who made it.
func TestToolProxy_Do_RecordsCallerIdentityOnLogAttributes(t *testing.T) {
	t.Parallel()

	_, logAttrs := callFunctionToolWithClient(t, toolconfig.MCPClientIdentity{
		Name:          "claude-code",
		Version:       "2.1",
		OAuthClientID: "client-abc",
		Capabilities:  []string{"elicitation", "roots"},
	})

	require.Equal(t, "claude-code", logAttrs[attr.McpClientNameKey])
	require.Equal(t, "2.1", logAttrs[attr.McpClientVersionKey])
	require.Equal(t, []string{"elicitation", "roots"}, logAttrs[attr.McpClientCapabilitiesKey])
	require.Equal(t, "client-abc", logAttrs[attr.OAuthClientIDKey])
}

// TestToolProxy_Do_OmitsUnreportedCallerIdentity keeps the row free of empty
// placeholders so a query can tell "not reported" from "reported as empty".
func TestToolProxy_Do_OmitsUnreportedCallerIdentity(t *testing.T) {
	t.Parallel()

	_, logAttrs := callFunctionToolWithClient(t, toolconfig.MCPClientIdentity{
		Name:          "",
		Version:       "",
		OAuthClientID: "client-abc",
		Capabilities:  nil,
	})

	require.NotContains(t, logAttrs, attr.McpClientNameKey)
	require.NotContains(t, logAttrs, attr.McpClientVersionKey)
	require.NotContains(t, logAttrs, attr.McpClientCapabilitiesKey)
	require.Equal(t, "client-abc", logAttrs[attr.OAuthClientIDKey])
}

// TestToolProxy_Do_FunctionTool_OAuthClientWithoutClientInfo covers a caller
// that authenticated but never reported an identity: the verified half must
// still reach the function.
func TestToolProxy_Do_FunctionTool_OAuthClientWithoutClientInfo(t *testing.T) {
	t.Parallel()

	meta, _ := callFunctionToolWithClient(t, toolconfig.MCPClientIdentity{
		Name:          "",
		Version:       "",
		OAuthClientID: "client-abc",
	})

	require.NotNil(t, meta)
	require.Nil(t, meta.ClientInfo)
	require.Equal(t, "client-abc", meta.OAuthClientID)
}

// TestToolProxy_Do_FunctionTool_UnknownCallerSendsNoMeta keeps direct
// (non-MCP) invocations byte-identical to what they sent before caller
// identity existed.
func TestToolProxy_Do_FunctionTool_UnknownCallerSendsNoMeta(t *testing.T) {
	t.Parallel()

	meta, _ := callFunctionToolWithClient(t, toolconfig.MCPClientIdentity{
		Name:          "",
		Version:       "",
		OAuthClientID: "",
	})

	require.Nil(t, meta)
}
