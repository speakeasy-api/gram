package platformmcp

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	testChatGPTConnectorClientID = "https://chatgpt.com/oauth/connector-id/client.json"
	testOtherClientID            = "https://client.example/oauth/client.json"
)

func TestIsChatGPTClient(t *testing.T) {
	t.Parallel()

	for clientID, want := range map[string]bool{
		testChatGPTConnectorClientID:                            true,
		"https://chatgpt.com/oauth/client.json":                 true,
		"https://chatgpt.com/oauth/codex/client.json":           false,
		"https://chatgpt.com/oauth/codex/server-id/client.json": false,
		testOtherClientID:                                       false,
		"00000000-0000-0000-0000-000000000000":                  false,
		"":                                                      false,
	} {
		require.Equal(t, want, isChatGPTClient(clientID), clientID)
	}
}

func TestChatGPTToolAllowlistNamesRegisteredExternalTools(t *testing.T) {
	t.Parallel()

	registered := names(newAllowlistTestRuntime(t, nil).registrar.For(AudienceExternal))
	for _, name := range chatGPTToolAllowlist {
		require.Contains(t, registered, name)
	}
}

func TestExternalCatalogueIntersectsChatGPTAllowlistWithGrants(t *testing.T) {
	t.Parallel()

	registrar := &Registrar{descriptors: []Descriptor{
		{Name: "search_mcp_catalog", Meta: ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly}},
		{Name: "register_catalog_mcp", Meta: ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, DiscoveryScopes: discoveryMCPWrite}},
		{Name: "list_projects", Meta: ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, DiscoveryScopes: discoveryProjectRead}},
		{Name: "get_platform_context", Meta: ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly}},
	}}
	tools := []*mcp.Tool{{Name: "search_mcp_catalog"}, {Name: "register_catalog_mcp"}, {Name: "list_projects"}, {Name: "get_platform_context"}}
	ctx := authz.GrantsToContext(t.Context(), []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, "project-1")})

	chatGPT := testPrincipal()
	chatGPT.ClientID = testChatGPTConnectorClientID
	require.Equal(t, []string{"search_mcp_catalog", "list_projects"}, toolNames(registrar.FilterExternalTools(ctx, chatGPT, tools)))

	other := testPrincipal()
	other.ClientID = testOtherClientID
	require.Equal(t, []string{"search_mcp_catalog", "list_projects", "get_platform_context"}, toolNames(registrar.FilterExternalTools(ctx, other, tools)))
}

func TestRuntimeToolsListLimitsChatGPTToAllowlist(t *testing.T) {
	t.Parallel()

	runtime := newAllowlistTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})
	handler := runtime.Handler()

	toolsList := func(clientID string) []string {
		var list struct {
			Tools []*mcp.Tool `json:"tools"`
		}
		allowlistRPC(t, handler, runtime, clientID, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, &list)
		return toolNames(list.Tools)
	}
	require.ElementsMatch(t, chatGPTToolAllowlist, toolsList(testChatGPTConnectorClientID))
	require.ElementsMatch(t, names(runtime.registrar.For(AudienceExternal)), toolsList(testOtherClientID), "other clients keep the full catalogue")
}

func TestRuntimeToolsCallRejectsToolsHiddenFromChatGPT(t *testing.T) {
	t.Parallel()

	runtime := newAllowlistTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})
	handler := runtime.Handler()
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_platform_context","arguments":{}}}`

	chatGPT := allowlistRPCRaw(t, handler, runtime, testChatGPTConnectorClientID, call)
	require.NotNil(t, chatGPT.Error)
	require.Equal(t, `unknown tool "get_platform_context"`, chatGPT.Error.Message)

	other := allowlistRPCRaw(t, handler, runtime, testOtherClientID, call)
	require.Nil(t, other.Error, "other clients can still call the tool")
}

func TestRuntimeInitializeDropsInstructionsForToolsHiddenFromChatGPT(t *testing.T) {
	t.Parallel()

	runtime := newAllowlistTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})
	handler := runtime.Handler()
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`

	var chatGPT, other struct {
		Instructions string `json:"instructions"`
	}
	allowlistRPC(t, handler, runtime, testChatGPTConnectorClientID, initialize, &chatGPT)
	allowlistRPC(t, handler, runtime, testOtherClientID, initialize, &other)

	require.Contains(t, other.Instructions, "distribute_mcp_to_plugin", "other clients keep every instruction")
	require.Less(t, len(chatGPT.Instructions), len(other.Instructions))
	require.Contains(t, chatGPT.Instructions, "attach_platform_mcp_identity_provider", "golden-path guidance stays")
	for _, descriptor := range runtime.registrar.Descriptors() {
		if slices.Contains(chatGPTToolAllowlist, descriptor.Name) {
			continue
		}
		require.NotRegexp(t, `\b`+descriptor.Name+`\b`, chatGPT.Instructions)
	}
}

func TestRuntimeDiscoverDropsInstructionsForToolsHiddenFromChatGPT(t *testing.T) {
	t.Parallel()

	runtime := newAllowlistTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})
	handler := runtime.Handler()
	discover := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"}}}}`
	header := http.Header{"Mcp-Protocol-Version": {"2026-07-28"}, "Mcp-Method": {"server/discover"}}

	var chatGPT, other struct {
		Instructions string `json:"instructions"`
		CacheScope   string `json:"cacheScope"`
	}
	allowlistRPC(t, handler, runtime, testChatGPTConnectorClientID, discover, &chatGPT, header)
	allowlistRPC(t, handler, runtime, testOtherClientID, discover, &other, header)

	require.Contains(t, other.Instructions, "distribute_mcp_to_plugin", "other clients keep every instruction")
	require.Less(t, len(chatGPT.Instructions), len(other.Instructions))
	require.NotContains(t, chatGPT.Instructions, "distribute_mcp_to_plugin")
	require.Equal(t, "private", chatGPT.CacheScope)
	require.Equal(t, "private", other.CacheScope)
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
	)
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
