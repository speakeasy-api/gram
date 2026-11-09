package risk_analysis

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
)

func TestLLMMessageSources_AppliesCategoryScopes(t *testing.T) {
	t.Parallel()

	messages := []batchMessage{
		msg(message.Assistant),
		msg(message.ToolResponse),
		msg(message.User),
	}
	masks := masksFor(t, nil, messages)
	covered := []string{SourceGitleaks, SourcePresidio, SourcePromptInjection}

	require.Empty(t, llmMessageSources(masks, 0, covered), "assistant messages are out of every recommended scope")
	require.Equal(t, covered, llmMessageSources(masks, 1, covered))
	require.Equal(t, covered, llmMessageSources(masks, 2, covered))
}

func TestLLMMessageSources_WithoutMasksKeepsEverySource(t *testing.T) {
	t.Parallel()

	covered := []string{SourceGitleaks, SourceCLIDestructive}
	require.Equal(t, covered, llmMessageSources(CategoryScopeMasks{categoryOut: nil}, 0, covered))
}

func TestCaptureRedactionMarkersAreNotFreshSecretEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		content    string
		markerOnly bool
	}{
		{content: "[redacted]", markerOnly: true},
		{content: " \n[redacted]\n[redacted] ", markerOnly: true},
		{content: "ordinary output", markerOnly: false},
		{content: "[redacted] and other evidence", markerOnly: false},
		{content: "", markerOnly: false},
	} {
		t.Run(tc.content, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.markerOnly, onlyCaptureMarkers(tc.content))
		})
	}
}
