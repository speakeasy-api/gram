package chat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNeedsClaudeTurnUsage(t *testing.T) {
	t.Parallel()

	ptr := func(s string) *string { return &s }

	require.False(t, needsClaudeTurnUsage(ptr("cursor"), nil))
	require.False(t, needsClaudeTurnUsage(ptr("codex"), nil))
	require.False(t, needsClaudeTurnUsage(nil, ptr("codex")))
	require.True(t, needsClaudeTurnUsage(ptr("claude-code"), nil))
	require.True(t, needsClaudeTurnUsage(ptr("litellm"), ptr("claude-code")))
	require.False(t, needsClaudeTurnUsage(ptr("litellm"), ptr("codex")))
	require.True(t, needsClaudeTurnUsage(ptr("litellm"), nil))
	require.True(t, needsClaudeTurnUsage(nil, nil))
	require.True(t, needsClaudeTurnUsage(ptr("new-agent-surface"), nil))
	require.True(t, needsClaudeTurnUsage(nil, ptr("unknown-client")))
}
