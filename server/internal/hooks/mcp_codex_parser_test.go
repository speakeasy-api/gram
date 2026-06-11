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

	// Codex emits tool names as mcp__<config name>__<tool>; the cached entry
	// must round-trip through matchCachedMCPEntry on that prefix.
	matched := matchCachedMCPEntry(entries, "int-linear")
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
