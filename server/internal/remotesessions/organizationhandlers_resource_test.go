// clientUpstreamResource derives the RFC 8707 resource the org-admin manual
// refresh sends: exactly one distinct non-empty upstream URL across the
// client's MCP servers binds the audience; anything else omits it.

package remotesessions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestClientUpstreamResource_NoRowsReturnsEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, clientUpstreamResource(nil))
}

func TestClientUpstreamResource_SingleURLTrimsTrailingSlash(t *testing.T) {
	t.Parallel()

	rows := []repo.ListOrganizationMcpServersForClientRow{
		{Url: "https://mcp.example.com/mcp/"},
	}
	require.Equal(t, "https://mcp.example.com/mcp", clientUpstreamResource(rows))
}

func TestClientUpstreamResource_DuplicateURLsCollapse(t *testing.T) {
	t.Parallel()

	rows := []repo.ListOrganizationMcpServersForClientRow{
		{Url: "https://mcp.example.com/mcp"},
		{Url: "https://mcp.example.com/mcp/"},
	}
	require.Equal(t, "https://mcp.example.com/mcp", clientUpstreamResource(rows))
}

func TestClientUpstreamResource_NonRemoteRowsIgnored(t *testing.T) {
	t.Parallel()

	rows := []repo.ListOrganizationMcpServersForClientRow{
		{Url: ""},
		{Url: "https://mcp.example.com/mcp"},
	}
	require.Equal(t, "https://mcp.example.com/mcp", clientUpstreamResource(rows))
}

func TestClientUpstreamResource_AllNonRemoteReturnsEmpty(t *testing.T) {
	t.Parallel()

	rows := []repo.ListOrganizationMcpServersForClientRow{
		{Url: ""},
		{Url: ""},
	}
	require.Empty(t, clientUpstreamResource(rows))
}

func TestClientUpstreamResource_MultipleDistinctURLsReturnsEmpty(t *testing.T) {
	t.Parallel()

	rows := []repo.ListOrganizationMcpServersForClientRow{
		{Url: "https://mcp.example.com/mcp"},
		{Url: "https://other.example.com/mcp"},
	}
	require.Empty(t, clientUpstreamResource(rows))
}
