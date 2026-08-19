package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalDirectRemoteURL(t *testing.T) {
	t.Parallel()

	canonical, err := canonicalDirectRemoteURL(" HTTPS://Example.TEST:443 ")

	require.NoError(t, err)
	require.Equal(t, "https://example.test/", canonical)
}

func TestCanonicalDirectRemoteURLRejectsUnsafeShapes(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{
		"http://example.test/mcp",
		"https://user:password@example.test/mcp",
		"https://example.test/mcp?token=value",
		"https://example.test/mcp#fragment",
		"https://example.test:8443/mcp",
		"https://example.test/{tenant}/mcp",
		"https://example.test/mcp\nX-Header: value",
	} {
		_, err := canonicalDirectRemoteURL(rawURL)
		require.ErrorIs(t, err, ErrDirectRemoteRejected, rawURL)
	}
}

func TestValidDirectRemoteRegistrationURLRequiresCanonicalForm(t *testing.T) {
	t.Parallel()

	require.True(t, validDirectRemoteRegistrationURL("https://example.test/mcp"))
	require.False(t, validDirectRemoteRegistrationURL("https://Example.test/mcp"))
	require.False(t, validDirectRemoteRegistrationURL("https://example.test:443/mcp"))
}
