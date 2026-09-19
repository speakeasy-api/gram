package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// validationFailureBody is the shape `@gram-ai/functions` produced for an input
// validation failure before the trim: a stack trace through the minified
// bundle, plus `error` carrying the `issues` array a second time as
// pretty-printed JSON.
const validationFailureBody = `{"error":"[\n  {\n    \"expected\": \"string\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"org_id\"\n    ],\n    \"message\": \"Invalid input: expected string, received undefined\"\n  }\n]","issues":[{"expected":"string","code":"invalid_type","path":["org_id"],"message":"Invalid input: expected string, received undefined"}],"stack":"ResponseError\n    at b1e (file:///var/task/functions.js:515:38295)\n    at fR.fail (file:///var/task/functions.js:515:38457)\n    at _r.handleToolCall (file:///var/task/functions.js:515:40340)"}`

func TestTrimFunctionFailureBody_ValidationFailure(t *testing.T) {
	t.Parallel()

	got := trimFunctionFailureBody([]byte(validationFailureBody))

	require.JSONEq(t, `{
		"error": "org_id: Invalid input: expected string, received undefined",
		"issues": [{
			"expected": "string",
			"code": "invalid_type",
			"path": ["org_id"],
			"message": "Invalid input: expected string, received undefined"
		}]
	}`, string(got))
	require.Less(t, len(got), len(validationFailureBody)/2, "trimmed body should be less than half the size of the original")
}

func TestTrimFunctionFailureBody_RunnerCauseStack(t *testing.T) {
	t.Parallel()

	got := trimFunctionFailureBody([]byte(`{
		"name": "FunctionsError",
		"message": "Tool call failed (gram_err_002)",
		"cause": {
			"name": "TypeError",
			"message": "Cannot read properties of undefined (reading 'id')",
			"stack": "TypeError: Cannot read properties of undefined (reading 'id')\n    at b1e (file:///var/task/functions.js:515:38295)"
		}
	}`))

	require.JSONEq(t, `{
		"name": "FunctionsError",
		"message": "Tool call failed (gram_err_002)",
		"cause": {
			"name": "TypeError",
			"message": "Cannot read properties of undefined (reading 'id')"
		}
	}`, string(got))
}

func TestTrimFunctionFailureBody_NestedStackInList(t *testing.T) {
	t.Parallel()

	got := trimFunctionFailureBody([]byte(`{"error":"batch failed","failures":[{"item":1,"stack":"Error: nope\n    at run (file:///var/task/functions.js:1:1)"}]}`))

	require.JSONEq(t, `{"error":"batch failed","failures":[{"item":1}]}`, string(got))
}

// A tool is free to report a value it calls a stack — a deployment stack, a
// protocol stack — and that is the tool's output, not a leaked trace.
func TestTrimFunctionFailureBody_KeepsStackFieldThatIsNotATrace(t *testing.T) {
	t.Parallel()

	body := []byte(`{"error":"deploy rejected","stack":"production-us-east"}`)

	require.Equal(t, body, trimFunctionFailureBody(body))
}

func TestTrimFunctionFailureBody_KeepsErrorThatIsNotAnIssuesDump(t *testing.T) {
	t.Parallel()

	body := []byte(`{"error":"the org could not be resolved","issues":[{"code":"invalid_type","path":["org_id"],"message":"Invalid input"}]}`)

	require.Equal(t, body, trimFunctionFailureBody(body))
}

func TestTrimFunctionFailureBody_KeepsBodyWithNothingToTrim(t *testing.T) {
	t.Parallel()

	for _, body := range [][]byte{
		[]byte(`{"error":"User not found"}`),
		[]byte(`[{"error":"not an object"}]`),
		[]byte(`not json at all`),
		[]byte(``),
	} {
		require.Equal(t, body, trimFunctionFailureBody(body))
	}
}

func TestFormatResult_TrimsFunctionFailure(t *testing.T) {
	t.Parallel()

	rw := failureResponseWriter(t, validationFailureBody)

	chunk, structured, err := formatResult(rw, gateway.ToolKindFunction)
	require.NoError(t, err)

	require.NotContains(t, string(chunk), "functions.js")
	require.NotContains(t, string(structured), "functions.js")

	var content struct {
		Text string `json:"text"`
	}
	require.NoError(t, json.Unmarshal(chunk, &content))
	require.JSONEq(t, string(structured), content.Text, "text content and structuredContent must agree")

	require.JSONEq(t, validationFailureBody, rw.body.String(), "the untrimmed body must survive for telemetry")
}

func TestFormatResult_KeepsFunctionSuccessBody(t *testing.T) {
	t.Parallel()

	body := `{"stack":"Error: fine\n    at run (file:///var/task/functions.js:1:1)"}`
	rw := failureResponseWriter(t, body)
	rw.statusCode = http.StatusOK

	chunk, structured, err := formatResult(rw, gateway.ToolKindFunction)
	require.NoError(t, err)

	require.Contains(t, string(chunk), "functions.js")
	require.JSONEq(t, body, string(structured))
}

// HTTP tools proxy an upstream API's own error body. Rewriting it would hide
// what the upstream actually said.
func TestFormatResult_KeepsHTTPToolFailureBody(t *testing.T) {
	t.Parallel()

	rw := failureResponseWriter(t, validationFailureBody)

	chunk, structured, err := formatResult(rw, gateway.ToolKindHTTP)
	require.NoError(t, err)

	require.Contains(t, string(chunk), "functions.js")
	require.JSONEq(t, validationFailureBody, string(structured))
}

// TestFunctionFailure_ThroughToolProxy covers the whole path a failed function
// call takes — runner response, gateway proxy, MCP formatting — since the trim
// only runs when the proxy has carried the runner's status onto the writer.
func TestFunctionFailure_ThroughToolProxy(t *testing.T) {
	t.Parallel()

	var invocationID uuid.UUID
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Gram-Invoke-ID", invocationID.String())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(validationFailureBody))
	}))
	t.Cleanup(runner.Close)

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	proxy := gateway.NewToolProxy(
		testenv.NewLogger(t),
		tracerProvider,
		testenv.NewMeterProvider(t),
		gateway.ToolCallSourceMCP,
		testenv.NewEncryptionClient(t),
		nil,
		policy,
		&stubFunctionRunner{
			serverURL:    runner.URL,
			invocationID: &invocationID,
		},
		nil,
	)

	plan := gateway.NewFunctionToolCallPlan(&gateway.ToolDescriptor{
		ID:               uuid.New().String(),
		Name:             "lookup",
		Description:      nil,
		DeploymentID:     uuid.New().String(),
		ProjectID:        uuid.New().String(),
		ProjectSlug:      "test-project",
		OrganizationID:   uuid.New().String(),
		OrganizationSlug: "test-org",
		URN:              urn.NewTool(urn.ToolKindFunction, "functions", "lookup"),
	}, &gateway.FunctionToolCallPlan{
		FunctionID:        uuid.New().String(),
		FunctionsAccessID: uuid.New().String(),
		Runtime:           "nodejs",
		InputSchema:       []byte{},
		Variables:         map[string]*functions.ManifestVariableAttributeV0{},
		AuthInput:         nil,
	})

	body, err := json.Marshal(gateway.ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 json.RawMessage(`{}`),
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	})
	require.NoError(t, err)

	rw := toolCallResponseWriter{
		statusCode: http.StatusOK,
		headers:    http.Header{},
		body:       &bytes.Buffer{},
	}
	require.NoError(t, proxy.Do(t.Context(), &rw, bytes.NewReader(body), toolconfig.ToolCallEnv{
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: "",
		MCPClient:  toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""},
	}, plan, tm.HTTPLogAttributes{}))

	require.Equal(t, http.StatusBadRequest, rw.statusCode)

	chunk, structured, err := formatResult(rw, plan.Kind)
	require.NoError(t, err)

	var content struct {
		Text string `json:"text"`
	}
	require.NoError(t, json.Unmarshal(chunk, &content))
	require.JSONEq(t, `{
		"error": "org_id: Invalid input: expected string, received undefined",
		"issues": [{
			"expected": "string",
			"code": "invalid_type",
			"path": ["org_id"],
			"message": "Invalid input: expected string, received undefined"
		}]
	}`, content.Text)
	require.NotContains(t, string(structured), "functions.js")
}

// stubFunctionRunner points the gateway at an HTTP test server standing in for
// a deployed function.
type stubFunctionRunner struct {
	serverURL    string
	invocationID *uuid.UUID
}

func (s *stubFunctionRunner) ToolCall(ctx context.Context, req functions.RunnerToolCallRequest) (*http.Request, error) {
	*s.invocationID = req.InvocationID

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.serverURL, bytes.NewReader(req.Input))
	if err != nil {
		return nil, fmt.Errorf("create function runner request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	return httpReq, nil
}

func (s *stubFunctionRunner) ReadResource(ctx context.Context, req functions.RunnerResourceReadRequest) (*http.Request, error) {
	*s.invocationID = req.InvocationID

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, s.serverURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create function runner request: %w", err)
	}

	return httpReq, nil
}

func failureResponseWriter(t *testing.T, body string) toolCallResponseWriter {
	t.Helper()

	headers := http.Header{}
	headers.Set("content-type", "application/json")

	return toolCallResponseWriter{
		statusCode: http.StatusBadRequest,
		headers:    headers,
		body:       bytes.NewBufferString(body),
	}
}
