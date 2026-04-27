package proxy

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
)

// decodeSubstitute is a small helper that runs the decoded SSE event payload
// through a generic JSON unmarshal so test assertions can inspect the
// resulting JSON-RPC envelope without coupling to specific SDK types.
func decodeSubstitute(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(payload, &out))
	return out
}

func TestSubstituteRejectedJSONRPCPayload_ResponseBecomesErrorResponseWithSameID(t *testing.T) {
	t.Parallel()

	id, err := jsonrpc.MakeID(float64(42))
	require.NoError(t, err)

	resp, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":42,"result":{"ok":true}}`))
	require.NoError(t, err)

	reject := &RejectError{Code: RejectCodeServerError, Message: "blocked by policy", Data: map[string]any{"reason": "pii"}}

	payload, err := substituteRejectedJSONRPCPayload(resp, reject)
	require.NoError(t, err)

	out := decodeSubstitute(t, payload)
	require.Equal(t, "2.0", out["jsonrpc"])
	require.EqualValues(t, id.Raw(), out["id"], "substitute must preserve the response id")
	require.Contains(t, out, "error")
	require.NotContains(t, out, "result", "error responses must not also carry result")

	errPayload, ok := out["error"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, RejectCodeServerError, errPayload["code"])
	require.Equal(t, "blocked by policy", errPayload["message"])
	require.Equal(t, map[string]any{"reason": "pii"}, errPayload["data"])
}

func TestSubstituteRejectedJSONRPCPayload_ServerInitiatedRequestBecomesErrorResponseWithSameID(t *testing.T) {
	t.Parallel()

	// MCP server-initiated request (sampling/createMessage). Has an id so
	// the user's MCP runtime can correlate the substitute back to the
	// outstanding inbound request.
	req, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":"abc-123","method":"sampling/createMessage","params":{"messages":[]}}`))
	require.NoError(t, err)

	reject := &RejectError{Code: RejectCodeServerError, Message: "sampling blocked", Data: nil}

	payload, err := substituteRejectedJSONRPCPayload(req, reject)
	require.NoError(t, err)

	out := decodeSubstitute(t, payload)
	require.Equal(t, "abc-123", out["id"])
	errPayload, ok := out["error"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, RejectCodeServerError, errPayload["code"])
	require.Equal(t, "sampling blocked", errPayload["message"])
}

func TestSubstituteRejectedJSONRPCPayload_NotificationBecomesNotificationsMessage(t *testing.T) {
	t.Parallel()

	notif, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p","progress":0.7}}`))
	require.NoError(t, err)

	reject := &RejectError{Code: RejectCodeServerError, Message: "progress notifications disabled", Data: nil}

	payload, err := substituteRejectedJSONRPCPayload(notif, reject)
	require.NoError(t, err)

	out := decodeSubstitute(t, payload)
	require.Equal(t, "2.0", out["jsonrpc"])
	require.NotContains(t, out, "id", "log notification substitutes must not carry an id")
	require.Equal(t, "notifications/message", out["method"])

	params, ok := out["params"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "error", params["level"])

	data, ok := params["data"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "progress notifications disabled", data["reason"])
	require.Equal(t, "notifications/progress", data["rejected_method"], "substitute must record the original method for observability")
}

func TestSubstituteRejectedSSEEvent_FramesPayloadAsSSE(t *testing.T) {
	t.Parallel()

	notif, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"p"}}`))
	require.NoError(t, err)

	reject := &RejectError{Code: RejectCodeServerError, Message: "blocked", Data: nil}

	rawEvent, err := substituteRejectedSSEEvent(notif, reject)
	require.NoError(t, err)

	// Must start with "data: " and end with the mandatory blank-line
	// separator so downstream SSE parsers treat it as a complete event.
	require.Greater(t, len(rawEvent), len("data: \n\n"))
	require.Equal(t, "data: ", string(rawEvent[:6]))
	require.Equal(t, "\n\n", string(rawEvent[len(rawEvent)-2:]))

	// The middle is a JSON-RPC payload that round-trips cleanly.
	jsonPayload := rawEvent[6 : len(rawEvent)-2]
	out := decodeSubstitute(t, jsonPayload)
	require.Equal(t, "notifications/message", out["method"])
}

func TestSubstituteRejectedJSONRPCPayload_NilRejectFallsBackToInternalError(t *testing.T) {
	t.Parallel()

	resp, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	require.NoError(t, err)

	payload, err := substituteRejectedJSONRPCPayload(resp, nil)
	require.NoError(t, err)

	out := decodeSubstitute(t, payload)
	errPayload, ok := out["error"].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, RejectCodeInternalError, errPayload["code"])
}
