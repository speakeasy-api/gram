package runner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/functions/internal/svc"
)

func TestExecutionTimeout(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{name: "absent uses default", seconds: 0, want: defaultExecutionTimeout},
		{name: "negative uses default", seconds: -5, want: defaultExecutionTimeout},
		{name: "within bounds", seconds: 30, want: 30 * time.Second},
		{name: "at ceiling", seconds: 600, want: maxExecutionTimeout},
		{name: "above ceiling is capped", seconds: 601, want: maxExecutionTimeout},
		{name: "huge value does not overflow", seconds: int(^uint(0) >> 1), want: maxExecutionTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, executionTimeout(tc.seconds))
		})
	}
}

func TestCallToolPayload_DecodesTimeout(t *testing.T) {
	t.Parallel()

	var payload CallToolPayload
	require.NoError(t, json.Unmarshal([]byte(`{"name": "slow", "input": {}, "timeout_seconds": 90}`), &payload))
	require.Equal(t, 90, payload.TimeoutSeconds)
}

// sleepBundle never answers within any timeout a test would wait for.
const sleepBundle = `
export async function handleToolCall(call) {
  await new Promise((resolve) => setTimeout(resolve, 60_000));
  return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
}
`

func TestCallTool_StopsAtRequestedTimeout(t *testing.T) {
	t.Parallel()

	svc := benchService(t, sleepBundle)
	recorder := httptest.NewRecorder()

	started := time.Now()
	err := svc.callTool(t.Context(), svc.logger, CallToolPayload{
		ToolName:       "slow",
		Input:          json.RawMessage(`{}`),
		Environment:    nil,
		Meta:           nil,
		TimeoutSeconds: 1,
	}, recorder)
	elapsed := time.Since(started)

	methodErr := requireMethodError(t, err)
	require.Equal(t, http.StatusRequestTimeout, methodErr.Code())
	require.True(t, methodErr.Timeout)
	require.False(t, methodErr.Temporary, "a timed-out call already ran; it must not invite a retry")
	require.Contains(t, methodErr.Message, "time limit of 1s")
	require.Less(t, elapsed, 30*time.Second)
}

// echoCallBundle returns the request the entrypoint handed to user code.
const echoCallBundle = `
export async function handleToolCall(call) {
  return new Response(JSON.stringify(call), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
`

func TestCallTool_TimeoutNeverReachesUserCode(t *testing.T) {
	t.Parallel()

	svc := benchService(t, echoCallBundle)
	recorder := httptest.NewRecorder()

	err := svc.callTool(t.Context(), svc.logger, CallToolPayload{
		ToolName:       "echo",
		Input:          json.RawMessage(`{"q": 1}`),
		Environment:    nil,
		Meta:           nil,
		TimeoutSeconds: 120,
	}, recorder)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"q":1`)
	require.NotContains(t, recorder.Body.String(), "timeout")
}

func requireMethodError(t *testing.T, err error) *svc.MethodError {
	t.Helper()

	var methodErr *svc.MethodError
	require.ErrorAs(t, err, &methodErr)
	return methodErr
}
