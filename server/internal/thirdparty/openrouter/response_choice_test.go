package openrouter

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// The web-search plugin's url_citation annotations live inside the message
// object, which the SDK union type cannot carry — ResponseChoice lifts them
// at decode time. The fixture is a captured OpenRouter response (message
// carrying content, reasoning, refusal, and annotations), so this breaks if
// either the lift or the SDK's union decode stops handling the real wire
// shape.
func TestResponseChoice_DecodesWebPluginAnnotations(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/web_plugin_response.json")
	require.NoError(t, err)

	var resp OpenAIChatResponse
	require.NoError(t, json.Unmarshal(raw, &resp))

	require.Len(t, resp.Choices, 1)
	choice := resp.Choices[0]

	require.NotEmpty(t, GetText(choice.Message), "the assistant message must decode alongside the annotations")

	require.Len(t, choice.Annotations, 3)
	first := choice.Annotations[0]
	require.Equal(t, "url_citation", first.Type)
	require.NotNil(t, first.URLCitation)
	require.Equal(t, "https://modelcontextprotocol.io/docs/tools/inspector", first.URLCitation.URL)
	require.Equal(t, "MCP Inspector - Model Context Protocol", first.URLCitation.Title)
	require.NotEmpty(t, first.URLCitation.Content, "the citation snippet is the search result's payload")
}

// A choice without a message (error envelopes) and a message without
// annotations both decode cleanly.
func TestResponseChoice_ToleratesSparseChoices(t *testing.T) {
	t.Parallel()

	var sparse ResponseChoice
	require.NoError(t, json.Unmarshal([]byte(`{"finish_reason": "stop"}`), &sparse))
	require.Equal(t, "stop", sparse.FinishReason)
	require.Empty(t, sparse.Annotations)

	var plain ResponseChoice
	require.NoError(t, json.Unmarshal([]byte(`{"message": {"role": "assistant", "content": "hi"}, "finish_reason": "stop"}`), &plain))
	require.Equal(t, "hi", GetText(plain.Message))
	require.Empty(t, plain.Annotations)
}
