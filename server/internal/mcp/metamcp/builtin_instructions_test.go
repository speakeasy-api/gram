package metamcp

import (
	"os"
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
