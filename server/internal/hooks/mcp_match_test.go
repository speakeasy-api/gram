package hooks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The tool-name prefix Claude Code derives from each entry of `claude mcp
// list` is unspecified — this table is calibrated against the real names
// observed in a live session (see fixtures in mcp_list_parser_test.go and
// the MCP tools loaded at the top of an interactive conversation).
func TestMCPServerPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source string
		plugin string
		raw    string
		want   string
	}{
		{"claude.ai simple", "claude.ai", "", "Slack", "claude_ai_Slack"},
		{"claude.ai with parens", "claude.ai", "", "Linear (Speakeasy)", "claude_ai_Linear_Speakeasy"},
		{"claude.ai multi-word + parens", "claude.ai", "", "HubSpot (Speakeasy MCP Platform)", "claude_ai_HubSpot_Speakeasy_MCP_Platform"},
		{"claude.ai parens with multi-word inner", "claude.ai", "", "Speakeasy MCP Server (Read only)", "claude_ai_Speakeasy_MCP_Server_Read_only"},
		{"claude.ai with hyphens", "claude.ai", "", "la-growth-machine", "claude_ai_la-growth-machine"},
		{"plugin double name", "plugin", "slack", "slack", "plugin_slack_slack"},
		{"plugin distinct name", "plugin", "github", "octocat-mcp", "plugin_github_octocat-mcp"},
		{"local plain", "local", "", "gram", "gram"},
		{"local with hyphen", "local", "", "notion-local", "notion-local"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, mcpServerPrefix(tc.source, tc.plugin, tc.raw))
		})
	}
}

func TestMatchCachedMCPEntry(t *testing.T) {
	t.Parallel()
	entries := []MCPServerEntry{
		{Source: "claude.ai", Name: "Linear (Speakeasy)", URL: "https://chat.speakeasy.com/mcp/linear"},
		{Source: "plugin", PluginName: "slack", Name: "slack", URL: "https://mcp.slack.com/mcp"},
		{Source: "local", Name: "gram", URL: "https://app.getgram.ai/mcp/team-foo"},
	}

	got := matchCachedMCPEntry(entries, "gram")
	if assert.NotNil(t, got) {
		assert.Equal(t, "https://app.getgram.ai/mcp/team-foo", got.URL)
	}

	assert.Nil(t, matchCachedMCPEntry(entries, "does_not_exist"))

	got = matchCachedMCPEntry(entries, "claude_ai_Linear_Speakeasy")
	if assert.NotNil(t, got) {
		assert.Equal(t, "https://chat.speakeasy.com/mcp/linear", got.URL)
	}
}

func TestIsGramHostedMCPURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		url  string
		want bool
	}{
		{"https://app.getgram.ai/mcp/team-foo", true},
		{"https://APP.GETGRAM.AI/mcp/team-foo", true}, // case-insensitive
		{"http://app.getgram.ai/mcp/team-foo", true},  // http allowed (rare but valid)
		{"https://chat.speakeasy.com/mcp/linear", false},
		{"https://evil.getgram.ai/mcp/x", false}, // subdomain squat must NOT pass
		{"https://mcp.slack.com/mcp", false},
		{"http://localhost:8080/mcp/x", false},
		{"", false},
		{"not a url at all", false},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isGramHostedMCPURL(tc.url))
		})
	}
}

func TestParseClaudeToolName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw    string
		server string
		tool   string
		isMCP  bool
	}{
		{"mcp__gram__do_thing", "gram", "do_thing", true},
		{"mcp__claude_ai_Linear_Speakeasy__list_issues", "claude_ai_Linear_Speakeasy", "list_issues", true},
		{"mcp__plugin_slack_slack__send_message", "plugin_slack_slack", "send_message", true},
		{"Read", "", "", false},
		{"Bash", "", "", false},
		{"mcp__only", "", "", false},
		{"mcp__server__", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got := parseClaudeToolName(tc.raw)
			assert.Equal(t, tc.isMCP, got.IsMCP)
			assert.Equal(t, tc.server, got.Server)
			assert.Equal(t, tc.tool, got.Tool)
		})
	}
}
