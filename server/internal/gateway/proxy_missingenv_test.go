package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// callFunctionToolWithEnv runs one function tool call that declares
// JAMF_TENANT_URL, JAMF_USERNAME and JAMF_PASSWORD against a runner answering
// runnerStatus, with userConfig as the caller's environment.
func callFunctionToolWithEnv(t *testing.T, runnerStatus int, userConfig map[string]string) (*httptest.ResponseRecorder, error) {
	t.Helper()

	var invocationID uuid.UUID
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(runnerStatus)
		_, _ = w.Write([]byte(`{"name":"ZodError","message":"[{\"path\":[\"JAMF_TENANT_URL\"]}]"}`))
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
		Variables: map[string]*functions.ManifestVariableAttributeV0{
			"JAMF_USERNAME":   {Description: nil},
			"JAMF_TENANT_URL": {Description: nil},
			"JAMF_PASSWORD":   {Description: nil},
		},
		AuthInput: nil,
	})

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
			onRequest: nil,
		},
		nil,
	)

	user := toolconfig.NewCaseInsensitiveEnv()
	for key, value := range userConfig {
		user.Set(key, value)
	}

	recorder := httptest.NewRecorder()
	err = proxy.Do(t.Context(), recorder, bytes.NewReader(json.RawMessage(`{"device":"1"}`)), toolconfig.ToolCallEnv{
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		UserConfig: user,
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: "",
		MCPClient:  toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""},
	}, toolCallPlan, tm.HTTPLogAttributes{})
	return recorder, err
}

// A function that fails while declared variables are unset answers with the
// variables to configure instead of the runner's validation output.
func TestToolProxy_Do_FunctionTool_FailureNamesMissingVariables(t *testing.T) {
	t.Parallel()

	_, err := callFunctionToolWithEnv(t, http.StatusInternalServerError, map[string]string{"JAMF_PASSWORD": "secret"})

	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeFailedPrecondition, shareable.Code)
	require.Equal(t, "server not configured: missing environment variables JAMF_TENANT_URL, JAMF_USERNAME", shareable.Error())
}

// Declared variables may be optional, so a function that succeeds without
// them is answered as it responded.
func TestToolProxy_Do_FunctionTool_SuccessWithMissingVariablesPassesThrough(t *testing.T) {
	t.Parallel()

	recorder, err := callFunctionToolWithEnv(t, http.StatusOK, nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
}

// A failing function whose variables are all set keeps the runner's own
// answer: the environment is not the cause.
func TestToolProxy_Do_FunctionTool_FailureWithAllVariablesPassesThrough(t *testing.T) {
	t.Parallel()

	recorder, err := callFunctionToolWithEnv(t, http.StatusInternalServerError, map[string]string{
		"JAMF_TENANT_URL": "https://tenant.example.com",
		"JAMF_USERNAME":   "admin",
		"JAMF_PASSWORD":   "secret",
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "ZodError")
}
