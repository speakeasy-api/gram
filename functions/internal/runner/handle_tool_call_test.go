package runner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// TestCallToolPayload_PartialCallerIdentity covers the shape most production
// traffic actually has. A public MCP server authenticates nobody, so calls
// arrive with a reported client and no OAuth client; an authenticated caller
// that never reported a name is the mirror image. Neither half depends on the
// other being present, and the absent one leaves no key behind.
func TestCallToolPayload_PartialCallerIdentity(t *testing.T) {
	t.Parallel()

	var clientOnly CallToolPayload
	require.NoError(t, json.Unmarshal([]byte(`{
	  "name": "whoami",
	  "input": {},
	  "_meta": {"io.modelcontextprotocol/clientInfo": {"name": "claude-code", "version": "2.1"}}
	}`), &clientOnly))
	require.Equal(t, &MCPClientInfo{Name: "claude-code", Version: "2.1"}, clientOnly.Meta.ClientInfo)
	require.Empty(t, clientOnly.Meta.OAuthClientID)

	encoded, err := json.Marshal(clientOnly)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "oauth-client-id")

	var oauthOnly CallToolPayload
	require.NoError(t, json.Unmarshal([]byte(`{
	  "name": "whoami",
	  "input": {},
	  "_meta": {"gram.ai/oauth-client-id": "client-abc"}
	}`), &oauthOnly))
	require.Nil(t, oauthOnly.Meta.ClientInfo)
	require.Equal(t, "client-abc", oauthOnly.Meta.OAuthClientID)

	encoded, err = json.Marshal(oauthOnly)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "clientInfo")
}

// echoCallerBundle returns whatever the entrypoint hands it as a second
// argument, which is how a tool sees its caller.
const echoCallerBundle = `
export async function handleToolCall(call, options) {
  return new Response(JSON.stringify(options ?? null), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
`

// TestCallTool_DeliversCallerIdentityToUserCode runs the whole runner path for
// real — payload decode, subprocess spawn, the embedded entrypoint, and user
// code — so the identity is proven to survive every hop rather than only the
// encoding steps the tests above pin.
func TestCallTool_DeliversCallerIdentityToUserCode(t *testing.T) {
	t.Parallel()

	svc := benchService(t, echoCallerBundle)
	recorder := httptest.NewRecorder()

	err := svc.callTool(t.Context(), svc.logger, CallToolPayload{
		ToolName:    "whoami",
		Input:       json.RawMessage(`{}`),
		Environment: nil,
		Meta: &ToolCallMeta{
			ClientInfo:    &MCPClientInfo{Name: "claude-code", Version: "2.1"},
			OAuthClientID: "client-abc",
		},
	}, recorder)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, recorder.Code)
	// The whole block rides along as `meta` beside the named fields, so a tool
	// can read keys the runner does not model.
	require.JSONEq(t, `{
	  "clientInfo": {"name": "claude-code", "version": "2.1"},
	  "oauthClientId": "client-abc",
	  "meta": {
	    "io.modelcontextprotocol/clientInfo": {"name": "claude-code", "version": "2.1"},
	    "gram.ai/oauth-client-id": "client-abc"
	  }
	}`, recorder.Body.String())
}

// TestCallTool_ClientWithoutOAuthReachesUserCode runs the public-MCP shape all
// the way into user code: an absent OAuth client must arrive as undefined, not
// as an empty string a tool might mistake for a real id.
func TestCallTool_ClientWithoutOAuthReachesUserCode(t *testing.T) {
	t.Parallel()

	svc := benchService(t, echoCallerBundle)
	recorder := httptest.NewRecorder()

	err := svc.callTool(t.Context(), svc.logger, CallToolPayload{
		ToolName:    "whoami",
		Input:       json.RawMessage(`{}`),
		Environment: nil,
		Meta: &ToolCallMeta{
			ClientInfo:    &MCPClientInfo{Name: "claude-code", Version: "2.1"},
			OAuthClientID: "",
		},
	}, recorder)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{
	  "clientInfo": {"name": "claude-code", "version": "2.1"},
	  "meta": {"io.modelcontextprotocol/clientInfo": {"name": "claude-code", "version": "2.1"}}
	}`, recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "oauthClientId")
}

func TestCallTool_UnknownCallerReachesUserCodeAsEmpty(t *testing.T) {
	t.Parallel()

	svc := benchService(t, echoCallerBundle)
	recorder := httptest.NewRecorder()

	err := svc.callTool(t.Context(), svc.logger, CallToolPayload{
		ToolName:    "whoami",
		Input:       json.RawMessage(`{}`),
		Environment: nil,
		Meta:        nil,
	}, recorder)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{}`, recorder.Body.String())
}
