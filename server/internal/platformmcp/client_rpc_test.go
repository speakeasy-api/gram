package platformmcp

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	testChatGPTConnectorClientID = "https://chatgpt.com/oauth/connector-id/client.json"
	testOtherClientID            = "https://client.example/oauth/client.json"
)

// ChatGPT is served the same catalogue and instructions as any other client:
// the ChatGPT app directory reviews every tool listed upfront, so it gets
// every tool as its own entry rather than a narrowed list.
func TestRuntimeServesChatGPTTheFullCatalogue(t *testing.T) {
	t.Parallel()

	runtime := newAllowlistTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})
	handler := runtime.Handler()
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`

	surface := func(clientID string) ([]string, string) {
		var list struct {
			Tools []*mcp.Tool `json:"tools"`
		}
		allowlistRPC(t, handler, runtime, clientID, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, &list)
		var initialized struct {
			Instructions string `json:"instructions"`
		}
		allowlistRPC(t, handler, runtime, clientID, initialize, &initialized)
		return toolNames(list.Tools), initialized.Instructions
	}
	chatGPTTools, chatGPTInstructions := surface(testChatGPTConnectorClientID)
	otherTools, otherInstructions := surface(testOtherClientID)
	require.ElementsMatch(t, names(runtime.registrar.For(AudienceExternal)), chatGPTTools)
	require.ElementsMatch(t, otherTools, chatGPTTools)
	require.Equal(t, otherInstructions, chatGPTInstructions)

	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_platform_context","arguments":{}}}`
	require.Nil(t, allowlistRPCRaw(t, handler, runtime, testChatGPTConnectorClientID, call).Error)
}

func newAllowlistTestRuntime(t *testing.T, grants []authz.Grant) *Runtime {
	t.Helper()

	return NewRuntime(
		testenv.NewLogger(t),
		&testAuthenticator{principal: testPrincipal()},
		testGate{enabled: true},
		&testAuthorizer{grants: grants},
		"",
		"test-cursor-key",
		nil,
		nil,
		nil,
		nil,
		nil,
	).WithAICatalog(aivendorstest.NewStore())
}

type allowlistRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// allowlistRPCRaw sends one JSON-RPC request as the given OAuth client. The
// authenticator is shared by the runtime, so requests run one at a time.
// Optional headers are added to the request.
func allowlistRPCRaw(t *testing.T, handler http.Handler, runtime *Runtime, clientID, body string, headers ...http.Header) allowlistRPCResponse {
	t.Helper()

	authenticator, ok := runtime.authenticator.(*testAuthenticator)
	require.True(t, ok)
	authenticator.principal.ClientID = clientID

	req := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer access-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for _, header := range headers {
		maps.Copy(req.Header, header)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())

	var response allowlistRPCResponse
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response), res.Body.String())
	return response
}

func allowlistRPC(t *testing.T, handler http.Handler, runtime *Runtime, clientID, body string, result any, headers ...http.Header) {
	t.Helper()

	response := allowlistRPCRaw(t, handler, runtime, clientID, body, headers...)
	require.Nil(t, response.Error)
	require.NoError(t, json.Unmarshal(response.Result, result))
}
