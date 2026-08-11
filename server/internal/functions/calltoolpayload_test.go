package functions

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCallToolPayload_CallerIdentityWireShape pins the exact keys Gram sends to
// a function runner. The runner decodes this body with its own mirrored type in
// `functions/internal/runner/handle_tool_call.go`, which cannot import this
// package — an identical literal there is what keeps the two in step, so a tag
// changed on one side fails a test rather than silently dropping the caller's
// identity in production.
func TestCallToolPayload_CallerIdentityWireShape(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(CallToolPayload{
		ToolName:    "whoami",
		Input:       json.RawMessage(`{"q":1}`),
		Environment: map[string]string{"API_KEY": "secret"},
		Meta: &ToolCallMeta{
			ClientInfo:    &MCPClientInfo{Name: "claude-code", Version: "2.1"},
			OAuthClientID: "client-abc",
		},
	})
	require.NoError(t, err)

	require.JSONEq(t, `{
	  "name": "whoami",
	  "input": {"q": 1},
	  "environment": {"API_KEY": "secret"},
	  "_meta": {
	    "io.modelcontextprotocol/clientInfo": {"name": "claude-code", "version": "2.1"},
	    "gram.ai/oauth-client-id": "client-abc"
	  }
	}`, string(encoded))
}

// TestCallToolPayload_OmitsMetaWhenUnknown keeps direct (non-MCP) invocations
// sending exactly what they sent before caller identity existed.
func TestCallToolPayload_OmitsMetaWhenUnknown(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(CallToolPayload{
		ToolName:    "whoami",
		Input:       json.RawMessage(`{}`),
		Environment: nil,
		Meta:        nil,
	})
	require.NoError(t, err)

	require.JSONEq(t, `{"name": "whoami", "input": {}}`, string(encoded))
}
