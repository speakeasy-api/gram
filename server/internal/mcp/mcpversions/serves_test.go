package mcpversions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

func TestHandshakeless(t *testing.T) {
	t.Parallel()

	for _, v := range []string{
		mcpversions.Version20241105,
		mcpversions.Version20250326,
		mcpversions.Version20250618,
		mcpversions.Version20251125,
	} {
		require.False(t, mcpversions.Handshakeless(v), "%s agrees a version at initialize", v)
	}

	require.True(t, mcpversions.Handshakeless(mcpversions.Version20260728),
		"2026-07-28 removed initialize and declares a version per request")
	require.False(t, mcpversions.Handshakeless(""), "an absent version is not a revision")
	require.False(t, mcpversions.Handshakeless("2099-01-01"), "an unrecognized version is not classifiable")
}

func TestServes(t *testing.T) {
	t.Parallel()

	served := mcpversions.Version20250326

	t.Run("requests this surface can honour", func(t *testing.T) {
		t.Parallel()

		for name, requested := range map[string]string{
			"the served revision itself":             mcpversions.Version20250326,
			"no declared revision":                   "",
			"an unrecognized revision":               "2099-01-01",
			"a newer revision that still handshakes": mcpversions.Version20250618,
			"the newest revision that handshakes":    mcpversions.Version20251125,
		} {
			require.True(t, mcpversions.Serves(served, requested), name)
		}
	})

	// A handshake-less client is never told what the server speaks, so serving it
	// a shape from another revision is undetectable on its side.
	t.Run("requests this surface cannot honour", func(t *testing.T) {
		t.Parallel()

		require.False(t, mcpversions.Serves(served, mcpversions.Version20260728))
	})

	// A surface that moves to the handshake-less revision serves it, and stops
	// serving the one it left behind only if that one is itself handshake-less.
	t.Run("follows the served revision", func(t *testing.T) {
		t.Parallel()

		require.True(t, mcpversions.Serves(mcpversions.Version20260728, mcpversions.Version20260728))
		require.True(t, mcpversions.Serves(mcpversions.Version20260728, mcpversions.Version20250326),
			"a handshake-era client adapts to whatever initialize answers")
	})
}
