package shadowmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeMatchValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		matchBreadth string
		matchValue   string
		want         string
	}{
		{
			name:         "full url lowercases scheme and host",
			matchBreadth: MatchBreadthFullURL,
			matchValue:   "HTTPS://Example.COM/mcp",
			want:         "https://example.com/mcp",
		},
		{
			name:         "full url strips default port and fragment",
			matchBreadth: MatchBreadthFullURL,
			matchValue:   "https://example.com:443/mcp#tools",
			want:         "https://example.com/mcp",
		},
		{
			name:         "full url keeps non default http port",
			matchBreadth: MatchBreadthFullURL,
			matchValue:   "http://example.com:443/mcp",
			want:         "http://example.com:443/mcp",
		},
		{
			name:         "full url keeps non default https port",
			matchBreadth: MatchBreadthFullURL,
			matchValue:   "https://example.com:80/mcp",
			want:         "https://example.com:80/mcp",
		},
		{
			name:         "full url normalizes empty root path",
			matchBreadth: MatchBreadthFullURL,
			matchValue:   "https://example.com/",
			want:         "https://example.com",
		},
		{
			name:         "full url sorts query keys",
			matchBreadth: MatchBreadthFullURL,
			matchValue:   "https://example.com/mcp?z=last&a=first",
			want:         "https://example.com/mcp?a=first&z=last",
		},
		{
			name:         "url host extracts host from url",
			matchBreadth: MatchBreadthURLHost,
			matchValue:   "HTTPS://Example.COM:443/path",
			want:         "example.com",
		},
		{
			name:         "url host keeps bare port without scheme",
			matchBreadth: MatchBreadthURLHost,
			matchValue:   "Example.COM:443",
			want:         "example.com:443",
		},
		{
			name:         "server identity trims and lowercases",
			matchBreadth: MatchBreadthServerIdentity,
			matchValue:   "  Linear MCP  ",
			want:         "linear mcp",
		},
		{
			name:         "server identity preserves separators while lowercasing",
			matchBreadth: MatchBreadthServerIdentity,
			matchValue:   "claude_ai_Calendly",
			want:         "claude_ai_calendly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeMatchValue(tt.matchBreadth, tt.matchValue)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeMatchValue_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		matchBreadth string
		matchValue   string
	}{
		{name: "empty match value", matchBreadth: MatchBreadthURLHost, matchValue: " "},
		{name: "invalid breadth", matchBreadth: "path", matchValue: "example.com"},
		{name: "full url requires scheme", matchBreadth: MatchBreadthFullURL, matchValue: "example.com/mcp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NormalizeMatchValue(tt.matchBreadth, tt.matchValue)
			require.Error(t, err)
		})
	}
}

func TestNormalizeAccessEvidence_DerivesURLHostWithSchemeAwarePortNormalization(t *testing.T) {
	t.Parallel()

	got := NormalizeAccessEvidence(AccessEvidence{
		FullURL:        "https://Example.COM:443/mcp",
		URLHost:        "",
		ServerIdentity: "",
	})

	require.Equal(t, "https://example.com/mcp", got.FullURL)
	require.Equal(t, "example.com", got.URLHost)
}
