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
