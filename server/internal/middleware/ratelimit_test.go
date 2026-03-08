package middleware

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractMCPKeyPublicRoute(t *testing.T) {
	key, ok := extractMCPKey("/mcp/my-server")
	require.True(t, ok)
	require.Equal(t, "my-server", key)
}

func TestExtractMCPKeyPublicRouteTrailingSlash(t *testing.T) {
	key, ok := extractMCPKey("/mcp/my-server/")
	require.True(t, ok)
	require.Equal(t, "my-server", key)
}

func TestExtractMCPKeyAuthenticatedRoute(t *testing.T) {
	key, ok := extractMCPKey("/mcp/my-project/my-toolset/production")
	require.True(t, ok)
	require.Equal(t, "my-project:my-toolset", key)
}

func TestExtractMCPKeyNonMCPPath(t *testing.T) {
	_, ok := extractMCPKey("/api/v1/tools")
	require.False(t, ok)
}

func TestExtractMCPKeyEmptySlug(t *testing.T) {
	_, ok := extractMCPKey("/mcp/")
	require.False(t, ok)
}

func TestExtractMCPKeyTwoSegments(t *testing.T) {
	// Two segments don't match either pattern.
	_, ok := extractMCPKey("/mcp/project/toolset")
	require.False(t, ok)
}

func TestExtractMCPKeyFourSegments(t *testing.T) {
	// Four segments don't match either pattern.
	_, ok := extractMCPKey("/mcp/a/b/c/d")
	require.False(t, ok)
}

func TestExtractMCPKeyRootPath(t *testing.T) {
	_, ok := extractMCPKey("/")
	require.False(t, ok)
}
