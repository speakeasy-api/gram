package aivendors

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// Mutates the package registry, so it must not run in parallel; parallel tests
// in this binary start only after every sequential top-level test returns.
func TestIDJAGDocumentsSkipDisabledDocuments(t *testing.T) { //nolint:paralleltest // swaps the package registry
	original := registry
	t.Cleanup(func() { registry = original })
	registry = slices.Clone(original)
	for i := range registry {
		registry[i].Documents = slices.Clone(registry[i].Documents)
		for j := range registry[i].Documents {
			if registry[i].Documents[j].URL == "https://chatgpt.com/oauth/codex/client.json" {
				registry[i].Documents[j].Enabled = false
			}
		}
	}

	_, ok := IDJAGDocument("https://chatgpt.com/oauth/codex/client.json")
	require.False(t, ok)
	for _, entry := range IDJAGDocuments() {
		require.NotEqual(t, "https://chatgpt.com/oauth/codex/client.json", entry.Document.URL)
	}
	_, ok = IDJAGDocument("https://claude.ai/oauth/claude-code-client-metadata")
	require.True(t, ok)
}
