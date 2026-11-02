package k8s

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomDomainBackendServices(t *testing.T) {
	t.Parallel()

	t.Run("empty backends use the historical server default", func(t *testing.T) {
		mcpBackend, serverBackend := customDomainBackendServices("", "")
		require.Equal(t, "gram-server", mcpBackend)
		require.Equal(t, "gram-server", serverBackend)
	})

	t.Run("server backend falls back to existing MCP backend", func(t *testing.T) {
		mcpBackend, serverBackend := customDomainBackendServices("custom-domain-mcp", "")
		require.Equal(t, "custom-domain-mcp", mcpBackend)
		require.Equal(t, "custom-domain-mcp", serverBackend)
	})

	t.Run("server backend can roll out independently", func(t *testing.T) {
		mcpBackend, serverBackend := customDomainBackendServices("custom-domain-mcp", "custom-domain-server")
		require.Equal(t, "custom-domain-mcp", mcpBackend)
		require.Equal(t, "custom-domain-server", serverBackend)
	})
}
