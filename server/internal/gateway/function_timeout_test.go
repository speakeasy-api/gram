package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// callFunctionToolWithTimeout runs one function tool call whose plan carries
// planSeconds and returns the timeout the proxy handed to the runner.
func callFunctionToolWithTimeout(t *testing.T, planSeconds int) int {
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
		TimeoutSeconds:    planSeconds,
	})

	captured := -1
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
			onRequest: func(req functions.RunnerToolCallRequest) { captured = req.TimeoutSeconds },
		},
		nil,
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

	err = proxy.Do(t.Context(), httptest.NewRecorder(), bytes.NewReader(bodyBytes), toolconfig.ToolCallEnv{
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		GramEmail:  "",
		GramChatID: "",
		MCPClient:  toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""},
	}, toolCallPlan, tm.HTTPLogAttributes{})
	require.NoError(t, err)

	return captured
}

func TestToolProxy_Do_FunctionTool_ForwardsOrganizationTimeout(t *testing.T) {
	t.Parallel()

	require.Equal(t, 300, callFunctionToolWithTimeout(t, 300))
}

func TestToolProxy_Do_FunctionTool_DefaultsTimeout(t *testing.T) {
	t.Parallel()

	// A plan without a limit, or with one outside the allowed range, gets the
	// platform default rather than the runner's own.
	cases := []struct {
		name        string
		planSeconds int
	}{
		{name: "unset", planSeconds: 0},
		{name: "below range", planSeconds: 5},
		{name: "above range", planSeconds: 3600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, 60, callFunctionToolWithTimeout(t, tc.planSeconds))
		})
	}
}

func TestFunctionRunnerCallTimeout_OutlastsTheInvocation(t *testing.T) {
	t.Parallel()

	require.Equal(t, 90*time.Second, functionRunnerCallTimeout(60))
	require.Equal(t, 10*time.Minute+30*time.Second, functionRunnerCallTimeout(600))
}
