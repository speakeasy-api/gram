package hooks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Real-world fixture captured from `claude mcp list` so the parser stays
// honest about the actual CLI output shape (claude.ai integrations,
// plugin-bundled servers with explicit transports, local HTTP servers).
const fixtureMCPList = `Checking MCP server health…

claude.ai Ahrefs: https://api.ahrefs.com/mcp/mcp - ! Needs authentication
claude.ai Linear (Speakeasy): https://chat.speakeasy.com/mcp/linear - ✓ Connected
plugin:github:github: https://api.githubcopilot.com/mcp/ (HTTP) - ✗ Failed to connect
plugin:slack:slack: https://mcp.slack.com/mcp (HTTP) - ! Needs authentication
madprocs: https://localhost:35294/mcp (HTTP) - ✗ Failed to connect
notion-local: http://localhost:8080/mcp/local-dev-org-rhviz (HTTP) - ✗ Failed to connect
some-stdio: /opt/bin/foo --flag (STDIO) - ✓ Connected
mise: mise mcp - ✓ Connected
`

func TestParseClaudeMCPList(t *testing.T) {
	t.Parallel()
	entries := ParseClaudeMCPList(fixtureMCPList)

	want := []MCPServerEntry{
		{Source: "claude.ai", Name: "Ahrefs", URL: "https://api.ahrefs.com/mcp/mcp", Transport: "HTTP", Status: "needs_auth"},
		{Source: "claude.ai", Name: "Linear (Speakeasy)", URL: "https://chat.speakeasy.com/mcp/linear", Transport: "HTTP", Status: "connected"},
		{Source: "plugin", PluginName: "github", Name: "github", URL: "https://api.githubcopilot.com/mcp/", Transport: "HTTP", Status: "failed"},
		{Source: "plugin", PluginName: "slack", Name: "slack", URL: "https://mcp.slack.com/mcp", Transport: "HTTP", Status: "needs_auth"},
		{Source: "local", Name: "madprocs", URL: "https://localhost:35294/mcp", Transport: "HTTP", Status: "failed"},
		{Source: "local", Name: "notion-local", URL: "http://localhost:8080/mcp/local-dev-org-rhviz", Transport: "HTTP", Status: "failed"},
		{Source: "local", Name: "some-stdio", Command: "/opt/bin/foo --flag", Transport: "STDIO", Status: "connected"},
		// No explicit "(STDIO)" suffix — the parser must still recognize a
		// non-URL target as a stdio command and default Transport.
		{Source: "local", Name: "mise", Command: "mise mcp", Transport: "STDIO", Status: "connected"},
	}

	if !assert.Len(t, entries, len(want)) {
		for i, e := range entries {
			t.Logf("entry[%d]: %+v", i, e)
		}
		return
	}
	for i, w := range want {
		got := entries[i]
		assert.Equal(t, w.Source, got.Source, "source[%d]", i)
		assert.Equal(t, w.PluginName, got.PluginName, "plugin_name[%d]", i)
		assert.Equal(t, w.Name, got.Name, "name[%d]", i)
		assert.Equal(t, w.URL, got.URL, "url[%d]", i)
		assert.Equal(t, w.Command, got.Command, "command[%d]", i)
		assert.Equal(t, w.Transport, got.Transport, "transport[%d]", i)
		assert.Equal(t, w.Status, got.Status, "status[%d]", i)
		assert.NotEmpty(t, got.RawLine, "raw_line[%d]", i)
		assert.NotEmpty(t, got.StatusRaw, "status_raw[%d]", i)
	}
}

func TestParseClaudeMCPList_SkipsPreambleAndJunk(t *testing.T) {
	t.Parallel()
	entries := ParseClaudeMCPList("Checking MCP server health…\n\n\nnot a valid line\n")
	assert.Empty(t, entries)
}
