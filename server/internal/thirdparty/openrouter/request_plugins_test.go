package openrouter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The plugins array is where two behaviours the research agent depends on are
// expressed on the wire, and both fail silently if the shape is wrong.
//
// Response healing matters most. Turning it off is what makes a malformed
// structured output reach the runner's validator unchanged; if this entry
// were dropped by omitempty, keyed wrongly, or never emitted, healing would
// stay on, and OpenRouter would repair the malformed extraction into
// schema-valid placeholder filler — the exact output the runner is built to
// fail loudly on. Nothing downstream can tell a healed report from a real
// one, so the assertion belongs here, at the boundary.
func TestRequestPlugins_WireShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		plugins  []RequestPlugin
		expected string
	}{
		{
			name:     "web search only",
			plugins:  []RequestPlugin{{ID: "web", Enabled: nil, MaxResults: 5}},
			expected: `[{"id":"web","max_results":5}]`,
		},
		{
			name: "response healing off",
			plugins: []RequestPlugin{
				{ID: "response-healing", Enabled: new(false), MaxResults: 0},
			},
			expected: `[{"id":"response-healing","enabled":false}]`,
		},
		{
			name: "both, in the order the request builds them",
			plugins: []RequestPlugin{
				{ID: "web", Enabled: nil, MaxResults: 3},
				{ID: "response-healing", Enabled: new(false), MaxResults: 0},
			},
			expected: `[{"id":"web","max_results":3},{"id":"response-healing","enabled":false}]`,
		},
		{
			// The web plugin's default-result case must not start emitting
			// max_results: 0, which asks for zero results rather than the
			// provider default.
			name:     "web search with the provider default",
			plugins:  []RequestPlugin{{ID: "web", Enabled: nil, MaxResults: 0}},
			expected: `[{"id":"web"}]`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(test.plugins)
			require.NoError(t, err)
			require.JSONEq(t, test.expected, string(encoded))
		})
	}
}

// enabled:false has to survive marshalling: it is a *bool precisely because
// omitempty would erase the false that carries the meaning.
func TestRequestPlugin_DisabledSurvivesOmitempty(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(RequestPlugin{ID: "response-healing", Enabled: new(false), MaxResults: 0})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"enabled":false`)
}
