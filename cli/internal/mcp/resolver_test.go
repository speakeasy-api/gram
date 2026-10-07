package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacyServerName(t *testing.T) {
	t.Parallel()

	legacy, ok := LegacyServerName(DefaultServerName)
	require.True(t, ok)
	require.Equal(t, "gram-mcp", legacy)

	legacy, ok = LegacyServerName(DefaultServerName + "-server")
	require.True(t, ok)
	require.Equal(t, "gram-mcp-server", legacy)

	_, ok = LegacyServerName("my-toolset")
	require.False(t, ok)

	// A URL-derived name that only starts with the default never maps, so
	// installing it can't remove an unrelated gram-mcp-* entry.
	for _, name := range []string{DefaultServerName + "-foo", DefaultServerName + "-server-2"} {
		_, ok = LegacyServerName(name)
		require.False(t, ok, name)
	}
}
