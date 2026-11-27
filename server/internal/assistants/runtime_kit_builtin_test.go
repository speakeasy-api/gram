package assistants

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestKitBuiltinDefaultThreadTargetIsPerTurn(t *testing.T) {
	t.Parallel()
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	for i, id := range ids {
		t.Run(id.String(), func(t *testing.T) {
			t.Parallel()
			prompt, err := kitProtocolPrompt(runTurnRequest{ThreadID: id, Prompt: "Use a built-in tool"}, false)
			require.NoError(t, err)
			require.Len(t, prompt, 2)
			require.Contains(t, prompt[0]["text"], id.String())
			require.NotContains(t, prompt[0]["text"], ids[1-i].String())
			require.Equal(t, "Use a built-in tool", prompt[1]["text"])
		})
	}
}

func TestKitBootstrapUsesSingleProjectGatewayAndNoBuiltinConnections(t *testing.T) {
	t.Parallel()
	gatewayURL, err := url.Parse("https://api.example/mcp/gateway/org-test/projects/proj-test")
	require.NoError(t, err)
	bootstrap := kitTestWorkloadBootstrap()
	bootstrap.MCPJSON, err = kitCompositionMCP(gatewayURL, bootstrap.Workload)
	require.NoError(t, err)
	require.NoError(t, validateKitBootstrap(bootstrap))

	var parsed struct {
		Servers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Auth    struct {
				Type          string            `json:"type"`
				Resource      string            `json:"resource"`
				TokenEndpoint string            `json:"tokenEndpoint"`
				Scope         string            `json:"scope"`
				Extensions    map[string]string `json:"extensions"`
			} `json:"auth"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(bootstrap.MCPJSON, &parsed))
	require.Len(t, parsed.Servers, 1, "Kit must have exactly one MCP entry: project-gateway")
	server, ok := parsed.Servers[kitProjectGatewayServerName]
	require.True(t, ok)
	require.Equal(t, "streamable-http", server.Type)
	require.Equal(t, gatewayURL.String(), server.URL)
	require.Equal(t, "workload_identity", server.Auth.Type)
	require.Equal(t, gatewayURL.String(), server.Auth.Resource)
	require.Equal(t, bootstrap.Workload.Audience+"/token", server.Auth.TokenEndpoint)
	require.Equal(t, "mcp:connect", server.Auth.Scope)
	require.Empty(t, server.Auth.Extensions)
	require.Empty(t, server.Headers)
	require.NotContains(t, string(bootstrap.MCPJSON), "builtin-")
	require.NotContains(t, string(bootstrap.MCPJSON), "urn:speakeasy:inference")
	require.NotContains(t, string(bootstrap.MCPJSON), string(bootstrap.GatewayToken))

	// Backend-agnostic composition allows non-workload-identity backends (e.g.
	// ATE proxy placeholder headers) to build the exact same single gateway
	// entry without GKE/jwtBearer fields.
	headerOnly, err := kitProjectGatewayMCPJSON(gatewayURL, map[string]string{"Authorization": "Bearer ${PLACEHOLDER}"}, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"mcpServers":{"project-gateway":{"type":"streamable-http","url":"https://api.example/mcp/gateway/org-test/projects/proj-test","headers":{"Authorization":"Bearer ${PLACEHOLDER}"}}}}`, string(headerOnly))
}
