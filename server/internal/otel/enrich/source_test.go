package enrich

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalSourcePassesThroughCanonicalSlugs(t *testing.T) {
	t.Parallel()

	require.Equal(t, "claude-code", CanonicalSource("claude-code"))
	require.Equal(t, "litellm", CanonicalSource("litellm"))
	require.Equal(t, "gram-server", CanonicalSource("gram-server"))
}

func TestCanonicalSourceSlugifiesServiceNames(t *testing.T) {
	t.Parallel()

	require.Equal(t, "claude-code", CanonicalSource("Claude Code"))
	require.Equal(t, "litellm", CanonicalSource("LiteLLM"))
	require.Equal(t, "my-service-v2", CanonicalSource("My_Service v2"))
	require.Equal(t, "claude-code", CanonicalSource("  claude-code  "))
}

func TestCanonicalSourceFoldsKnownAliases(t *testing.T) {
	t.Parallel()

	// ClaudeCode is a known product-surface alias; plain slugification would
	// produce claudecode.
	require.Equal(t, "claude-code", CanonicalSource("ClaudeCode"))
}

func TestCanonicalSourceFallsBackToUnknown(t *testing.T) {
	t.Parallel()

	require.Equal(t, SourceUnknown, CanonicalSource(""))
	require.Equal(t, SourceUnknown, CanonicalSource("   "))
	require.Equal(t, SourceUnknown, CanonicalSource("---"))
}
