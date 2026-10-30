package remotesessions

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTokenResponseScopesForIssuer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, issuer, scope string
		want                []string
	}{
		{"Slack", "https://mcp.slack.com", "channels:read,chat:write", []string{"channels:read", "chat:write"}},
		{"Slack whitespace", "https://mcp.slack.com", " channels:read, chat:write,, ", []string{"channels:read", "chat:write"}},
		{"Slack canonical spelling", " HTTPS://MCP.SLACK.COM:443/ ", "read,write", []string{"read", "write"}},
		{"Slack other path", "https://mcp.slack.com/other", "read,write", []string{"read,write"}},
		{"Slack other port", "https://mcp.slack.com:8443", "read,write", []string{"read,write"}},
		{"Slack query", "https://mcp.slack.com?other", "read,write", []string{"read,write"}},
		{"generic comma", "https://example.com", "read,write other", []string{"read,write", "other"}},
		{"Slack lookalike", "https://mcp.slack.com.example.com", "read,write", []string{"read,write"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, (tokenResponse{Scope: tc.scope}).scopesForIssuer(tc.issuer))
		})
	}
}
