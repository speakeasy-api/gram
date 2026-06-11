package hooks

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Wire shape verified against `codex mcp list --json` from codex-cli 0.139.0.
const codexMCPListJSON = `[
  {
    "name": "int-linear",
    "enabled": true,
    "disabled_reason": null,
    "transport": {
      "type": "streamable_http",
      "url": "https://chat.example.com/mcp/int-linear",
      "bearer_token_env_var": null,
      "http_headers": null,
      "env_http_headers": null
    },
    "startup_timeout_sec": null,
    "tool_timeout_sec": null,
    "auth_status": "o_auth"
  },
  {
    "name": "node_repl",
    "enabled": true,
    "disabled_reason": null,
    "transport": {
      "type": "stdio",
      "command": "/usr/local/bin/node_repl",
      "args": ["--port", "9000"],
      "env": {"FOO": "bar"}
    },
    "startup_timeout_sec": null,
    "tool_timeout_sec": null,
    "auth_status": "unsupported"
  },
  {
    "name": "disabled-server",
    "enabled": false,
    "disabled_reason": "user",
    "transport": {
      "type": "streamable_http",
      "url": "https://mcp.example.com/disabled"
    },
    "auth_status": "unsupported"
  }
]`

func TestParseCodexMCPList(t *testing.T) {
	t.Parallel()

	var raw any
	require.NoError(t, json.Unmarshal([]byte(codexMCPListJSON), &raw))

	got := ParseCodexMCPList(raw)
	require.Len(t, got, 2, "disabled servers must be skipped")

	require.Equal(t, "int-linear", got[0].Name)
	require.Equal(t, "https://chat.example.com/mcp/int-linear", got[0].URL)
	require.Empty(t, got[0].Command)
	require.Equal(t, "HTTP", got[0].Transport)
	require.Equal(t, "local", got[0].Source)
	require.Equal(t, "unknown", got[0].Status)
	require.Equal(t, "o_auth", got[0].StatusRaw)
	require.Equal(t, "int_linear", got[0].ToolPrefix, "ToolPrefix must use Codex's sanitizer: non-[A-Za-z0-9_] becomes underscore")

	require.Equal(t, "node_repl", got[1].Name)
	require.Empty(t, got[1].URL)
	require.Equal(t, "/usr/local/bin/node_repl --port 9000", got[1].Command)
	require.Equal(t, "STDIO", got[1].Transport)
}

func TestParseCodexMCPList_EntryMatchesCodexToolNamePrefix(t *testing.T) {
	t.Parallel()

	var raw any
	require.NoError(t, json.Unmarshal([]byte(codexMCPListJSON), &raw))
	entries := ParseCodexMCPList(raw)

	// Codex emits tool names as mcp__<sanitized config name>__<tool>, where
	// every character outside [A-Za-z0-9_] becomes "_" (codex-rs
	// sanitize_responses_api_tool_name) — "int-linear" arrives as
	// "int_linear". The cached entry must round-trip through
	// matchCachedMCPEntry on that sanitized prefix.
	matched := matchCachedMCPEntry(entries, "int_linear")
	require.NotNil(t, matched)
	require.Equal(t, "https://chat.example.com/mcp/int-linear", matched.URL)
}

func TestParseCodexMCPList_MalformedInput(t *testing.T) {
	t.Parallel()

	require.Nil(t, ParseCodexMCPList(nil))
	require.Nil(t, ParseCodexMCPList("not-an-array"))
	require.Empty(t, ParseCodexMCPList([]any{
		"not-a-map",
		map[string]any{"enabled": true}, // no name
		map[string]any{"name": "no-transport", "enabled": true},                       // no target
		map[string]any{"name": "empty", "transport": map[string]any{"type": "stdio"}}, // no url or command
	}))
}

func TestMatchCodexCachedMCPEntry_DoubleUnderscorePrefix(t *testing.T) {
	t.Parallel()

	entries := ParseCodexMCPList([]any{
		map[string]any{
			"name":    "foo--bar",
			"enabled": true,
			"transport": map[string]any{
				"type": "streamable_http",
				"url":  "https://mcp.example.com/foo-bar",
			},
		},
		map[string]any{
			"name":    "int-linear",
			"enabled": true,
			"transport": map[string]any{
				"type": "streamable_http",
				"url":  "https://chat.example.com/mcp/int-linear",
			},
		},
	})
	require.Len(t, entries, 2)
	require.Equal(t, "foo__bar", entries[0].ToolPrefix)

	// "foo--bar" sanitizes to "foo__bar", so a naive 3-way split of the tool
	// name yields prefix "foo" — longest-ToolPrefix matching must still
	// resolve the entry.
	matched := matchCodexCachedMCPEntry(entries, "mcp__foo__bar__run")
	require.NotNil(t, matched)
	require.Equal(t, "https://mcp.example.com/foo-bar", matched.URL)

	matched = matchCodexCachedMCPEntry(entries, "mcp__int_linear__get_issue")
	require.NotNil(t, matched)
	require.Equal(t, "https://chat.example.com/mcp/int-linear", matched.URL)

	require.Nil(t, matchCodexCachedMCPEntry(entries, "mcp__unknown__tool"))
	require.Nil(t, matchCodexCachedMCPEntry(entries, "Bash"))
}
