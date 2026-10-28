package aivendors_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

func TestIDJAGDocumentsAreExactlyClaudeCodeAndCodex(t *testing.T) {
	t.Parallel()

	var got []string
	for _, entry := range aivendors.IDJAGDocuments() {
		got = append(got, entry.Product.DisplayName+" "+entry.Document.URL)
	}
	require.Equal(t, []string{
		"Claude Code https://claude.ai/oauth/claude-code-client-metadata",
		"Codex https://chatgpt.com/oauth/codex/client.json",
	}, got)
}

func TestIDJAGIsNeverSetOnPatternDocuments(t *testing.T) {
	t.Parallel()

	for _, entry := range aivendors.CIMDDocuments() {
		if strings.Contains(entry.Document.URL, "*") {
			require.False(t, entry.Document.IDJAG, entry.Document.URL)
		}
	}
}

func TestIDJAGDocumentMatchesExactly(t *testing.T) {
	t.Parallel()

	_, ok := aivendors.IDJAGDocument("https://chatgpt.com/oauth/codex/client.json")
	require.True(t, ok)
	for _, clientID := range []string{"https://chatgpt.com/oauth/codex/abc/client.json", "https://claude.ai/oauth/claude-code-client-metadata/", ""} {
		_, ok := aivendors.IDJAGDocument(clientID)
		require.False(t, ok, clientID)
	}
}

func TestIDJAGProductForDocumentNamesTheProductToUpdate(t *testing.T) {
	t.Parallel()

	product, ok := aivendors.IDJAGProductForDocument("https://chatgpt.com/oauth/codex/*/client.json")
	require.True(t, ok)
	require.Equal(t, "Codex", product.DisplayName)
	_, ok = aivendors.IDJAGProductForDocument("https://chatgpt.com/oauth/*/client.json")
	require.False(t, ok)
}
