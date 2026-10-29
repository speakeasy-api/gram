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
		{"generic comma", "https://example.com", "read,write other", []string{"read,write", "other"}},
		{"Slack lookalike", "https://mcp.slack.com.example.com", "read,write", []string{"read,write"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, (tokenResponse{Scope: tc.scope}).scopesForIssuer(tc.issuer))
		})
	}
}
