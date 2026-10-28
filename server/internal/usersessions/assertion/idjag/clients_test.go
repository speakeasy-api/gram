package idjag_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

func TestAllowedClientsAreExactCatalogEntries(t *testing.T) {
	t.Parallel()

	clients := idjag.AllowedClients()
	require.NotEmpty(t, clients)
	for _, client := range clients {
		preset, ok := admission.CatalogPreset(client.ClientID)
		require.True(t, ok, client.ClientID)
		require.Equal(t, client.ClientID, preset.URL, "must match a literal catalog entry, not a pattern")
		require.True(t, idjag.ClientAllowed(client.ClientID))
	}
}

func TestClientAllowedMatchesExactly(t *testing.T) {
	t.Parallel()

	require.False(t, idjag.ClientAllowed("https://chatgpt.com/oauth/codex/abc/client.json"))
	require.False(t, idjag.ClientAllowed("https://claude.ai/oauth/claude-code-client-metadata/"))
	require.False(t, idjag.ClientAllowed(""))
}
