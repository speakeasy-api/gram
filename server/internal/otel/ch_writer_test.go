package otel

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanonicalEventSourcePassesThroughCanonicalSlugs(t *testing.T) {
	t.Parallel()

	require.Equal(t, "claude-code", canonicalEventSource("claude-code"))
	require.Equal(t, "litellm", canonicalEventSource("litellm"))
	require.Equal(t, "gram-server", canonicalEventSource("gram-server"))
}

func TestCanonicalEventSourceSlugifiesServiceNames(t *testing.T) {
	t.Parallel()

	require.Equal(t, "claude-code", canonicalEventSource("Claude Code"))
	require.Equal(t, "litellm", canonicalEventSource("LiteLLM"))
	require.Equal(t, "my-service-v2", canonicalEventSource("My_Service v2"))
	require.Equal(t, "claude-code", canonicalEventSource("  claude-code  "))
}

func TestCanonicalEventSourceFoldsKnownAliases(t *testing.T) {
	t.Parallel()

	// ClaudeCode is a known product-surface alias; plain slugification would
	// produce claudecode.
	require.Equal(t, "claude-code", canonicalEventSource("ClaudeCode"))
}

func TestCanonicalEventSourceFallsBackToUnknown(t *testing.T) {
	t.Parallel()

	require.Equal(t, "unknown", canonicalEventSource(""))
	require.Equal(t, "unknown", canonicalEventSource("   "))
	require.Equal(t, "unknown", canonicalEventSource("---"))
}

func TestHexEventIDTreatsEmptyAndZeroAsAbsent(t *testing.T) {
	t.Parallel()

	require.Empty(t, hexEventID(nil))
	require.Empty(t, hexEventID([]byte{}))
	require.Empty(t, hexEventID(make([]byte, 16)))
	require.Equal(t, "0102030405060708", hexEventID([]byte{1, 2, 3, 4, 5, 6, 7, 8}))
}
