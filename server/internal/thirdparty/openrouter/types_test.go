package openrouter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenAIChatRequestTemperatureZeroIsSent guards against re-introducing
// `omitempty` on Temperature. A caller requesting temperature 0 (deterministic
// sampling) must have it sent on the wire; with omitempty the zero value is
// dropped and the provider silently falls back to its non-zero default, which
// defeats callers like pijudge that rely on temperature 0 for stable verdicts.
func TestOpenAIChatRequestTemperatureZeroIsSent(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(OpenAIChatRequest{Model: "anthropic/claude-haiku-4.5", Temperature: 0})
	require.NoError(t, err)
	require.Contains(t, string(data), `"temperature":0`)
}

// TestOpenAIChatRequestTemperatureNonZeroIsSent confirms an explicit non-zero
// temperature still serializes as-is.
func TestOpenAIChatRequestTemperatureNonZeroIsSent(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(OpenAIChatRequest{Model: "anthropic/claude-haiku-4.5", Temperature: 0.7})
	require.NoError(t, err)
	require.Contains(t, string(data), `"temperature":0.7`)
}

// TestOpenAIChatRequestProviderRouting confirms provider routing is sent only
// when set, with fallbacks off stated explicitly.
func TestOpenAIChatRequestProviderRouting(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(OpenAIChatRequest{Model: "anthropic/claude-sonnet-5.5", Provider: &ProviderRouting{Order: []string{"anthropic"}, AllowFallbacks: false}})
	require.NoError(t, err)
	require.Contains(t, string(data), `"provider":{"order":["anthropic"],"allow_fallbacks":false}`)

	data, err = json.Marshal(OpenAIChatRequest{Model: "anthropic/claude-sonnet-5.5"})
	require.NoError(t, err)
	require.NotContains(t, string(data), `"provider"`)
}

// TestWithProviderRoutingKeepsItsOwnCopy confirms a caller changing its
// routing afterwards does not change the client's requests.
func TestWithProviderRoutingKeepsItsOwnCopy(t *testing.T) {
	t.Parallel()

	routing := &ProviderRouting{Order: []string{"amazon-bedrock"}, AllowFallbacks: false}
	client := (&ChatClient{}).WithProviderRouting(routing)
	routing.Order[0] = "google-vertex"
	routing.AllowFallbacks = true

	require.Equal(t, &ProviderRouting{Order: []string{"amazon-bedrock"}, AllowFallbacks: false}, client.providerRouting)
	require.Nil(t, client.WithProviderRouting(nil).providerRouting)
}
