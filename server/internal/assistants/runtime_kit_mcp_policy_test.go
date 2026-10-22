package assistants

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKitTurnMCPServersUsesScopedCredential(t *testing.T) {
	t.Parallel()
	for _, servers := range [][]runtimeMCPServer{nil, {}, {{ID: "platform", URL: "https://example.invalid/platform"}}, {{ID: "platform", URL: "https://example.invalid/platform"}, {ID: "attached", URL: "https://example.invalid/mcp", Headers: map[string]string{"authorization": "stale", "X-Environment": "test"}}}} {
		got := kitTurnMCPServers(runTurnRequest{AuthToken: "turn-credential", MCPServers: servers})
		require.Len(t, got, len(servers))
		for i, server := range got {
			require.Equal(t, servers[i].ID, server["name"])
			headers, ok := server["headers"].([]map[string]string)
			require.True(t, ok)
			require.Contains(t, headers, map[string]string{"name": "Authorization", "value": "Bearer turn-credential"})
			for _, header := range headers {
				require.NotEqual(t, "stale", header["value"])
			}
		}
	}
}
