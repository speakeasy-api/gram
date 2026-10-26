package metamcp

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The dashboard ships a copy of the built-in instructions so operators can
// copy them into the editor; keep it byte-identical to what the gateway serves.
func TestBuiltInInstructionsMatchDashboardCopy(t *testing.T) {
	t.Parallel()

	canonical, err := os.ReadFile("../../../../client/dashboard/src/pages/mcp/gateway/builtin-gateway-instructions.txt")
	require.NoError(t, err)
	require.Equal(t, Instructions, string(canonical), "update client/dashboard/src/pages/mcp/gateway/builtin-gateway-instructions.txt after editing metamcp.Instructions")
}

func TestCodeInstructionsMatchDashboardCopy(t *testing.T) {
	t.Parallel()
	canonical, err := os.ReadFile("../../../../client/dashboard/src/pages/mcp/gateway/builtin-gateway-code-instructions.txt")
	require.NoError(t, err)
	// Only the file's final newline is not part of the served instructions.
	require.True(t, strings.HasSuffix(string(canonical), "\n"), "builtin-gateway-code-instructions.txt must end with a newline")
	require.Equal(t, ResolveDiscoveryInstructions(nil, DiscoveryModeCode), strings.TrimSuffix(string(canonical), "\n"))
}
