package researchagent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHarvestHTTPSURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			// Compact JSON is the stored-evidence shape: quote-delimited
			// values with no whitespace between fields.
			name: "compact json keeps each url separate",
			text: `{"homepage":"https://vendor.example/docs","repo":"https://github.com/vendor/mcp"}`,
			want: []string{"https://vendor.example/docs", "https://github.com/vendor/mcp"},
		},
		{
			name: "prose punctuation is shed",
			text: `See https://vendor.example/security, then https://vendor.example/trust.`,
			want: []string{"https://vendor.example/security", "https://vendor.example/trust"},
		},
		{
			name: "a paren the url opened stays",
			text: `Read https://en.wikipedia.org/wiki/MCP_(protocol) for background (also https://vendor.example/faq).`,
			want: []string{"https://en.wikipedia.org/wiki/MCP_(protocol)", "https://vendor.example/faq"},
		},
		{
			name: "a bare scheme is not a url",
			text: `the prefix https:// alone means nothing`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, harvestHTTPSURLs(tt.text))
		})
	}
}
