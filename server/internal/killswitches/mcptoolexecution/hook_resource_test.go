package mcptoolexecution

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/delegation"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

func TestHookActivityResourceDeriveRejectsRawResourceKeys(t *testing.T) {
	t.Parallel()
	adapter := HookActivityResourceAdapter{}

	raw, err := adapter.Derive(t.Context(), killswitches.OrganizationID("org-1"), "claude:pre_tool_use")
	require.NoError(t, err)
	_, supported, err := raw.Key()
	require.NoError(t, err)
	require.False(t, supported)

	verified, err := adapter.Derive(t.Context(), killswitches.OrganizationID("org-1"), HookActivitySource{Provider: delegation.ProviderClaude, Event: delegation.EventPreToolUse})
	require.NoError(t, err)
	key, supported, err := verified.Key()
	require.NoError(t, err)
	require.True(t, supported)
	require.Equal(t, killswitches.ResourceKey("claude:pre_tool_use"), key)
}
