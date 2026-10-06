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

// GRW-253: a provider that matches the resource exactly against its RFC 9728
// resource rejects a trimmed one, so the registered URL is sent verbatim.
func TestClientUpstreamResource_SingleURLKeepsTrailingSlash(t *testing.T) {
	t.Parallel()

	rows := []repo.ListOrganizationMcpServersForClientRow{
		{Url: "https://mcp.example.com/"},
	}
	require.Equal(t, "https://mcp.example.com/", clientUpstreamResource(rows))
}

func TestClientUpstreamResource_SingleURLWithoutTrailingSlashGainsNone(t *testing.T) {
	t.Parallel()

	rows := []repo.ListOrganizationMcpServersForClientRow{
		{Url: "https://mcp.example.com"},
	}
	require.Equal(t, "https://mcp.example.com", clientUpstreamResource(rows))
}

func TestClientUpstreamResource_DuplicateURLsCollapseToShortestInAnyOrder(t *testing.T) {
	t.Parallel()

	slashed := repo.ListOrganizationMcpServersForClientRow{Url: "https://mcp.example.com/mcp/"}
	bare := repo.ListOrganizationMcpServersForClientRow{Url: "https://mcp.example.com/mcp"}
	require.Equal(t, "https://mcp.example.com/mcp", clientUpstreamResource([]repo.ListOrganizationMcpServersForClientRow{bare, slashed}))
	require.Equal(t, "https://mcp.example.com/mcp", clientUpstreamResource([]repo.ListOrganizationMcpServersForClientRow{slashed, bare}))
}

func TestClaimableUpstream_ClaimsUpstreamVerbatim(t *testing.T) {
	t.Parallel()

	own := []repo.ListOrganizationMcpServersForClientRow{
		{Url: "https://mcp.example.com/"},
		{Url: "https://other.example.com/mcp"},
	}
	resource, claimable := claimableUpstream(own, "https://mcp.example.com/")
	require.True(t, claimable)
	require.Equal(t, "https://mcp.example.com/", resource)
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
