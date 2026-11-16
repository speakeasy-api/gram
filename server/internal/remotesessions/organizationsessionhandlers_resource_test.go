// clientUpstreamResource derives the RFC 8707 resource the org-admin manual
// refresh sends: exactly one distinct non-empty upstream across the client's
// MCP servers binds the audience; anything else omits it.

package remotesessions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientUpstreamResource_NoRowsReturnsEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, clientUpstreamResource(nil))
}

// GRW-253: a provider that matches the resource exactly against its RFC 9728
// resource rejects a trimmed one, so the registered URL is sent verbatim.
func TestClientUpstreamResource_SingleURLKeepsTrailingSlash(t *testing.T) {
	t.Parallel()

	resources := []string{
		"https://mcp.example.com/",
	}
	require.Equal(t, "https://mcp.example.com/", clientUpstreamResource(resources))
}

func TestClientUpstreamResource_SingleURLWithoutTrailingSlashGainsNone(t *testing.T) {
	t.Parallel()

	resources := []string{
		"https://mcp.example.com",
	}
	require.Equal(t, "https://mcp.example.com", clientUpstreamResource(resources))
}

func TestClientUpstreamResource_DuplicateURLsCollapseToShortestInAnyOrder(t *testing.T) {
	t.Parallel()

	slashed := "https://mcp.example.com/mcp/"
	bare := "https://mcp.example.com/mcp"
	require.Equal(t, "https://mcp.example.com/mcp", clientUpstreamResource([]string{bare, slashed}))
	require.Equal(t, "https://mcp.example.com/mcp", clientUpstreamResource([]string{slashed, bare}))
}

func TestClaimableUpstream_ClaimsUpstreamVerbatim(t *testing.T) {
	t.Parallel()

	own := []string{
		"https://mcp.example.com/",
		"https://other.example.com/mcp",
	}
	resource, claimable := claimableUpstream(own, "https://mcp.example.com/")
	require.True(t, claimable)
	require.Equal(t, "https://mcp.example.com/", resource)
}

func TestClientUpstreamResource_EmptyResourcesIgnored(t *testing.T) {
	t.Parallel()

	resources := []string{
		"",
		"https://mcp.example.com/mcp",
	}
	require.Equal(t, "https://mcp.example.com/mcp", clientUpstreamResource(resources))
}

func TestClientUpstreamResource_AllEmptyReturnsEmpty(t *testing.T) {
	t.Parallel()

	resources := []string{
		"",
		"",
	}
	require.Empty(t, clientUpstreamResource(resources))
}

func TestClientUpstreamResource_MultipleDistinctURLsReturnsEmpty(t *testing.T) {
	t.Parallel()

	resources := []string{
		"https://mcp.example.com/mcp",
		"https://other.example.com/mcp",
	}
	require.Empty(t, clientUpstreamResource(resources))
}
