package catalog

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSlugFromName_DerivesFromLastSegment(t *testing.T) {
	t.Parallel()

	require.Equal(t, "exa", slugFromName("io.modelcontextprotocol.anonymous/exa"))
	require.Equal(t, "my-server", slugFromName("My Server!"))
	require.Equal(t, "my-server", slugFromName("ai.example/My Server??"))
}

func TestSlugFromName_RejectsAllSeparators(t *testing.T) {
	t.Parallel()

	require.Empty(t, slugFromName("---"))
	require.Empty(t, slugFromName("ai.example/!!!"))
}

func TestDefaultDisplayName_PrefersSpecifierTail(t *testing.T) {
	t.Parallel()

	require.Equal(t, "exa", defaultDisplayName("io.modelcontextprotocol.anonymous/exa", "ignored"))
	require.Equal(t, "server", defaultDisplayName("server", "ignored"))
	require.Equal(t, "fallback", defaultDisplayName("", "fallback"))
}
