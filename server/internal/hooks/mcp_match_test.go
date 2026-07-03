package hooks

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/speakeasy-api/gram/server/internal/attr"
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
		{Source: "claude.ai", Name: "Slack", URL: "https://mcp.example.com/slack", ConnectorUUID: "a1b2c3d4-e5f6-7890-abcd-ef0123456789"},
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

	// Cowork inventory: tool names use the connector UUID as the server
	// prefix, so the matcher resolves the entry by its ConnectorUUID field.
	got = matchCachedMCPEntry(entries, "a1b2c3d4-e5f6-7890-abcd-ef0123456789")
	if assert.NotNil(t, got) {
		assert.Equal(t, "Slack", got.Name)
		assert.Equal(t, "https://mcp.example.com/slack", got.URL)
	}
}

func TestMatchCachedMCPEntry_AmbiguousPrefix(t *testing.T) {
	t.Parallel()
	entries := []MCPServerEntry{
		{Source: "local", Name: "datadog", URL: "https://app.getgram.ai/mcp/datadog", ToolPrefix: "datadog"},
		{Source: "local", Name: "datadog", URL: "https://third-party.example.com/mcp/datadog", ToolPrefix: "datadog"},
	}

	assert.Nil(t, matchCachedMCPEntry(entries, "datadog"))
}

func TestMatchCodexCachedMCPServerEntry(t *testing.T) {
	t.Parallel()
	entries := []MCPServerEntry{
		{Source: "local", Name: "platform-logs", URL: "https://chat.example.com/mcp/platform-logs", ToolPrefix: "platform_logs"},
		{Source: "local", Name: "Slack (Remote)", URL: "https://app.getgram.ai/mcp/slack", ToolPrefix: "Slack__Remote_"},
	}

	got := matchCodexCachedMCPServerEntry(entries, "platform-logs")
	if assert.NotNil(t, got) {
		assert.Equal(t, "https://chat.example.com/mcp/platform-logs", got.URL)
	}

	got = matchCodexCachedMCPServerEntry(entries, "Slack (Remote)")
	if assert.NotNil(t, got) {
		assert.Equal(t, "https://app.getgram.ai/mcp/slack", got.URL)
	}

	got = matchCodexCachedMCPServerEntry(entries, "platform_logs")
	if assert.NotNil(t, got) {
		assert.Equal(t, "platform-logs", got.Name)
	}

	assert.Nil(t, matchCodexCachedMCPServerEntry(entries, ""))
	assert.Nil(t, matchCodexCachedMCPServerEntry(entries, "missing"))
}

func TestMatchCodexCachedMCPServerEntry_AmbiguousName(t *testing.T) {
	t.Parallel()
	entries := []MCPServerEntry{
		{Source: "local", Name: "Datadog", URL: "https://app.getgram.ai/mcp/datadog", ToolPrefix: "Datadog"},
		{Source: "local", Name: "Datadog", URL: "https://third-party.example.com/mcp/datadog", ToolPrefix: "Datadog"},
	}

	assert.Nil(t, matchCodexCachedMCPServerEntry(entries, "Datadog"))
}

func TestApplyMCPInventoryAttrs(t *testing.T) {
	t.Parallel()

	t.Run("nil matched leaves attrs untouched", func(t *testing.T) {
		t.Parallel()
		attrs := map[attr.Key]any{attr.ToolCallSourceKey: "some-uuid"}
		applyMCPInventoryAttrs(attrs, nil)
		assert.Equal(t, "some-uuid", attrs[attr.ToolCallSourceKey])
		_, hasURL := attrs[attr.MCPServerURLKey]
		assert.False(t, hasURL)
	})

	t.Run("claude.ai entry overrides sanitized source with raw name", func(t *testing.T) {
		t.Parallel()
		attrs := map[attr.Key]any{attr.ToolCallSourceKey: "claude_ai_Slack"}
		applyMCPInventoryAttrs(attrs, &MCPServerEntry{
			Source: "claude.ai", Name: "Slack", URL: "https://mcp.example.com/slack",
		})
		assert.Equal(t, "Slack", attrs[attr.ToolCallSourceKey])
		assert.Equal(t, "https://mcp.example.com/slack", attrs[attr.MCPServerURLKey])
	})

	t.Run("cowork entry overrides uuid source with name", func(t *testing.T) {
		t.Parallel()
		attrs := map[attr.Key]any{attr.ToolCallSourceKey: "a1b2c3d4-uuid"}
		applyMCPInventoryAttrs(attrs, &MCPServerEntry{
			Source: "claude.ai", Name: "Slack", URL: "https://mcp.example.com/slack",
			ConnectorUUID: "a1b2c3d4-uuid",
			ToolPrefix:    "",
		})
		assert.Equal(t, "Slack", attrs[attr.ToolCallSourceKey])
		assert.Equal(t, "https://mcp.example.com/slack", attrs[attr.MCPServerURLKey])
	})

	t.Run("entry without name leaves source intact", func(t *testing.T) {
		t.Parallel()
		attrs := map[attr.Key]any{attr.ToolCallSourceKey: "a1b2c3d4-uuid"}
		applyMCPInventoryAttrs(attrs, &MCPServerEntry{
			Source: "claude.ai", URL: "https://mcp.example.com/slack",
			ConnectorUUID: "a1b2c3d4-uuid",
			ToolPrefix:    "",
		})
		assert.Equal(t, "a1b2c3d4-uuid", attrs[attr.ToolCallSourceKey])
		assert.Equal(t, "https://mcp.example.com/slack", attrs[attr.MCPServerURLKey])
	})
}

func TestIsGramHostedMCPURL(t *testing.T) {
	t.Parallel()

	t.Run("canonical host only", func(t *testing.T) {
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
	})

	t.Run("with additional trusted hosts", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			url         string
			extraHosts  []string
			want        bool
			description string
		}{
			{"https://chat.speakeasy.com/mcp/linear", []string{"chat.speakeasy.com"}, true, "custom domain matches"},
			{"https://CHAT.SPEAKEASY.COM/mcp/linear", []string{"chat.speakeasy.com"}, true, "custom domain case-insensitive"},
			{"https://localhost:8080/mcp/local-org", []string{"localhost"}, true, "configured local Gram server host matches"},
			{"https://app.getgram.ai/mcp/x", []string{"chat.speakeasy.com"}, true, "canonical still works with extra hosts"},
			{"https://other.example.com/mcp/x", []string{"chat.speakeasy.com"}, false, "unknown host rejected"},
			{"https://mcp.slack.com/mcp", []string{"chat.speakeasy.com"}, false, "third party rejected"},
			{"", []string{"chat.speakeasy.com"}, false, "empty URL"},
		}
		for _, tc := range cases {
			t.Run(tc.description, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tc.want, isGramHostedMCPURL(tc.url, tc.extraHosts...))
			})
		}
	})
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
		{"mcp____do_thing", "", "", false},
		{"MCP:send_message", "", "", false},
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
