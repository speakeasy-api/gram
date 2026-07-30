package runner

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// gramToolCallBody is the request body Gram sends for a tool call carrying
// caller identity. Kept as a literal so a rename of any JSON tag on either
// side of the wire fails loudly here rather than silently dropping the
// caller's identity in production.
const gramToolCallBody = `{
  "name": "whoami",
  "input": {"q": 1},
  "environment": {"API_KEY": "secret"},
  "_meta": {
    "io.modelcontextprotocol/clientInfo": {"name": "claude-code", "version": "2.1"},
    "gram.ai/oauth-client-id": "client-abc"
  }
}`

func TestCallToolPayload_DecodesCallerIdentity(t *testing.T) {
	t.Parallel()

	var payload CallToolPayload
	require.NoError(t, json.Unmarshal([]byte(gramToolCallBody), &payload))

	require.Equal(t, "whoami", payload.ToolName)
	require.NotNil(t, payload.Meta)
	require.Equal(t, "client-abc", payload.Meta.OAuthClientID)
	require.Equal(t, &MCPClientInfo{Name: "claude-code", Version: "2.1"}, payload.Meta.ClientInfo)
}

// TestCallToolPayload_ReencodeDropsUnknownMeta pins the reason `_meta` is a
// declared type rather than raw bytes: the entrypoint arguments are re-encoded
// from this struct, so keys we never declared cannot ride along into the
// subprocess.
func TestCallToolPayload_ReencodeDropsUnknownMeta(t *testing.T) {
	t.Parallel()

	var payload CallToolPayload
	require.NoError(t, json.Unmarshal([]byte(`{
	  "name": "whoami",
	  "input": {},
	  "_meta": {
	    "io.modelcontextprotocol/clientInfo": {"name": "claude-code", "version": "2.1"},
	    "example.com/smuggled": "payload"
	  }
	}`), &payload))

	// Mirrors what callTool sends to the entrypoint: everything but the
	// environment, re-encoded from the decoded struct.
	payload.Environment = nil
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)

	require.NotContains(t, string(encoded), "smuggled")
	require.Contains(t, string(encoded), `"io.modelcontextprotocol/clientInfo":{"name":"claude-code","version":"2.1"}`)
}

// TestCallToolPayload_OmitsMetaWhenAbsent keeps calls with no caller identity
// byte-identical to what the runner received before `_meta` existed, so older
// entrypoints see nothing new.
func TestCallToolPayload_OmitsMetaWhenAbsent(t *testing.T) {
	t.Parallel()

	var payload CallToolPayload
	require.NoError(t, json.Unmarshal([]byte(`{"name": "whoami", "input": {}}`), &payload))
	require.Nil(t, payload.Meta)

	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "_meta")
}
