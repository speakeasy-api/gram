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
}
