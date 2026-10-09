package plugins

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// codexServerKey is the name pattern Codex enforces for MCP servers. It is
// also a bare TOML key, so `[mcp_servers.<name>]` needs no quoting, and a
// valid JSON object key for Claude Code and Cursor.
var codexServerKey = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func TestDeviceMCPServerName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		slug        string
		displayName string
		want        string
	}{
		{name: "slug wins over display name", slug: "linear", displayName: "Team Linear", want: "speakeasy-linear"},
		{name: "slug is lowercased", slug: "GitHub", want: "speakeasy-github"},
		{name: "disallowed runs collapse to one dash", slug: "acme__linear  (prod)", want: "speakeasy-acme-linear-prod"},
		{name: "dash runs collapse", slug: "acme---linear", want: "speakeasy-acme-linear"},
		{name: "ends are trimmed", slug: "-_linear_-", want: "speakeasy-linear"},
		{name: "display name when there is no slug", displayName: "Team Linear", want: "speakeasy-team-linear"},
		{name: "display name when the slug reduces to nothing", slug: "___", displayName: "Slack", want: "speakeasy-slack"},
		{name: "non-ASCII letters are separators", displayName: "Café Notes", want: "speakeasy-caf-notes"},
		{name: "fallback when nothing survives", displayName: "!!!", want: "speakeasy-mcp-server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := deviceMCPServerName(tc.slug, tc.displayName)
			require.Equal(t, tc.want, got)
			require.Regexp(t, codexServerKey, got)
		})
	}
}
