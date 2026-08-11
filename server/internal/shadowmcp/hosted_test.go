package shadowmcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestIsGramHostedMCPURL_CanonicalHosts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"canonical app host", "https://app.getgram.ai/mcp/team-foo", true},
		{"canonical app host uppercase", "https://APP.GETGRAM.AI/mcp/team-foo", true},
		{"canonical app host over http", "http://app.getgram.ai/mcp/team-foo", true},
		{"canonical chat host", "https://chat.speakeasy.com/mcp/linear", true},
		{"canonical chat host uppercase", "https://CHAT.SPEAKEASY.COM/mcp/linear", true},
		{"subdomain squat rejected", "https://evil.getgram.ai/mcp/x", false},
		{"third party rejected", "https://mcp.slack.com/mcp", false},
		{"unconfigured localhost rejected", "http://localhost:8080/mcp/x", false},
		{"empty url", "", false},
		{"unparseable url", "not a url at all", false},
		// A Gram-shaped path on a foreign host must never pass: the check is
		// on the host, never the path.
		{"gram path on foreign host rejected", "https://evil.example.com/mcp/team-foo", false},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, shadowmcp.IsGramHostedMCPURL(tc.url), tc.name)
	}
}

func TestIsGramHostedMCPURL_AdditionalTrustedHosts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		url        string
		extraHosts []string
		want       bool
	}{
		{"custom domain matches", "https://docs.example.com/mcp/linear", []string{"docs.example.com"}, true},
		{"custom domain case-insensitive", "https://DOCS.EXAMPLE.COM/mcp/linear", []string{"docs.example.com"}, true},
		{"configured host and port matches", "https://localhost:8080/mcp/local-org", []string{"localhost:8080"}, true},
		{"different port stays external", "https://localhost:35294/mcp/local-shadow", []string{"localhost:8080"}, false},
		{"canonical still works with extra hosts", "https://app.getgram.ai/mcp/x", []string{"chat.speakeasy.com"}, true},
		{"unknown host rejected", "https://other.example.com/mcp/x", []string{"chat.speakeasy.com"}, false},
		{"third party rejected", "https://mcp.slack.com/mcp", []string{"chat.speakeasy.com"}, false},
		{"empty url", "", []string{"chat.speakeasy.com"}, false},
		{"trusted host given as full url", "https://docs.example.com/mcp/x", []string{"https://docs.example.com"}, true},
		{"blank trusted host ignored", "https://other.example.com/mcp/x", []string{"  "}, false},
	}

	for _, tc := range cases {
		require.Equal(t, tc.want, shadowmcp.IsGramHostedMCPURL(tc.url, tc.extraHosts...), tc.name)
	}
}
