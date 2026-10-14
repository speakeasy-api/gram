package platformmcp

import (
	"encoding/json"
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

	registered := toolNamesOf(newAllowlistTestRuntime(t, nil).registrar.For(AudienceExternal))
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
	}}
	tools := []*mcp.Tool{{Name: "search_mcp_catalog"}, {Name: "register_catalog_mcp"}, {Name: "list_projects"}}
	ctx := authz.GrantsToContext(t.Context(), []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, "project-1")})

	chatGPT := testPrincipal()
	chatGPT.ClientID = testChatGPTConnectorClientID
	require.Equal(t, []string{"search_mcp_catalog"}, toolNames(registrar.FilterExternalTools(ctx, chatGPT, tools)))

	other := testPrincipal()
	other.ClientID = testOtherClientID
	require.Equal(t, []string{"search_mcp_catalog", "list_projects"}, toolNames(registrar.FilterExternalTools(ctx, other, tools)))
}

func TestRuntimeToolsListLimitsChatGPTToAllowlist(t *testing.T) {
	t.Parallel()

	runtime := newAllowlistTestRuntime(t, []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})
	handler := runtime.Handler()

	var chatGPTList struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	allowlistRPC(t, handler, runtime, testChatGPTConnectorClientID, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, &chatGPTList)
	chatGPTNames := make([]string, 0, len(chatGPTList.Tools))
	for _, tool := range chatGPTList.Tools {
		chatGPTNames = append(chatGPTNames, tool.Name)
	}
	require.ElementsMatch(t, chatGPTToolAllowlist, chatGPTNames)

	var otherList struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	allowlistRPC(t, handler, runtime, testOtherClientID, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, &otherList)
	otherNames := make([]string, 0, len(otherList.Tools))
	for _, tool := range otherList.Tools {
		otherNames = append(otherNames, tool.Name)
	}
	require.ElementsMatch(t, toolNamesOf(runtime.registrar.For(AudienceExternal)), otherNames, "other clients keep the full catalogue")
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
func allowlistRPCRaw(t *testing.T, handler http.Handler, runtime *Runtime, clientID, body string) allowlistRPCResponse {
	t.Helper()

	authenticator, ok := runtime.authenticator.(*testAuthenticator)
	require.True(t, ok)
	authenticator.principal.ClientID = clientID

	req := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer access-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())

	var response allowlistRPCResponse
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &response), res.Body.String())
	return response
}

func allowlistRPC(t *testing.T, handler http.Handler, runtime *Runtime, clientID, body string, result any) {
	t.Helper()

	response := allowlistRPCRaw(t, handler, runtime, clientID, body)
	require.Nil(t, response.Error)
	require.NoError(t, json.Unmarshal(response.Result, result))
}

func toolNamesOf(descriptors []Descriptor) []string {
	names := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		names = append(names, descriptor.Name)
	}
	return names
}
