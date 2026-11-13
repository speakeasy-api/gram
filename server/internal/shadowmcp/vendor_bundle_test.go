package shadowmcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestIsVendorBundleCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{
			name:    "codex computer use repl",
			command: "/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node /Applications/ChatGPT.app/Contents/Resources/cua_node/lib/node_modules/@oai/cua-repl/bin/cua-repl.mjs",
			want:    true,
		},
		{
			name:    "codex unified computer use launcher",
			command: "/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node --import /Applications/ChatGPT.app/Contents/Resources/plugins/unified-computer-use/26.903.61454/scripts/launch.mjs",
			want:    true,
		},
		{name: "codex node repl", command: "/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl", want: true},
		{name: "codex app bundle", command: "/Applications/Codex.app/Contents/Resources/bin/server", want: true},
		{name: "claude app bundle", command: "/Applications/Claude.app/Contents/Helpers/mcp-server", want: true},
		{name: "cursor app bundle", command: "/Applications/Cursor.app/Contents/Resources/app/bin/mcp", want: true},
		{
			name:    "codex bundled plugin cache, home relative",
			command: "~/.codex/plugins/cache/openai-bundled/codex-app-tools/1.2.3/scripts/launch_codex_app_tools_mcp ./server.mjs",
			want:    true,
		},
		{
			name:    "codex bundled plugin cache, absolute",
			command: "/Users/dev/.codex/plugins/cache/openai-bundled/codex-app-tools/1.2.3/scripts/launch_codex_app_tools_mcp ./server.mjs",
			want:    true,
		},
		{
			name:    "claude desktop managed directory",
			command: "/Users/dev/Library/Application Support/Claude/claude-code/2.1.0/claude mcp serve",
			want:    true,
		},
		{
			name:    "claude desktop managed directory via HOME",
			command: "$HOME/Library/Application Support/Claude/claude-code/2.1.0/claude mcp serve",
			want:    true,
		},
		{name: "codex computer use relative app", command: "./Codex Computer Use.app/Contents/MacOS/Codex Computer Use --mcp", want: true},
		{
			name:    "shell wrapper with export and exec",
			command: "/bin/sh -c export BROWSER_USE_ACCESS_EXECUTOR_ID=*** exec /Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node /Applications/ChatGPT.app/Contents/Resources/cua_node/lib/cua-repl.mjs",
			want:    true,
		},
		{
			name:    "shell wrapper with semicolon before exec",
			command: "/bin/sh -c export EXECUTOR_ID=abc123; exec /Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl",
			want:    true,
		},
		{name: "login shell wrapper without exec", command: "bash -lc /Applications/Codex.app/Contents/Resources/bin/server --stdio", want: true},
		{name: "env prefix", command: "env MODE=stdio /Applications/Codex.app/Contents/Resources/bin/server", want: true},

		{name: "user npx server", command: "npx -y @acme/mcp-server", want: false},
		{name: "system node running a vendor script", command: "node /Applications/ChatGPT.app/Contents/Resources/cua-repl.mjs", want: false},
		{name: "lookalike app name", command: "/Applications/ChatGPT.app.evil/bin/server", want: false},
		{name: "user application", command: "/Applications/Acme.app/Contents/MacOS/mcp", want: false},
		{name: "path climbing out of the bundle", command: "/Applications/ChatGPT.app/../../tmp/evil", want: false},
		{name: "user codex plugin cache", command: "~/.codex/plugins/cache/acme/tool/1.0.0/bin/server", want: false},
		{name: "bundled segment only in an argument", command: "/usr/local/bin/server /Users/dev/.codex/plugins/cache/openai-bundled/x", want: false},
		{
			name:    "shell wrapper running a second command",
			command: "/bin/sh -c /Applications/Codex.app/Contents/Resources/bin/server; curl https://example.test",
			want:    false,
		},
		{
			name:    "shell wrapper with command substitution",
			command: "/bin/sh -c export X=$(curl https://example.test) exec /Applications/Codex.app/Contents/Resources/bin/server",
			want:    false,
		},
		{name: "shell wrapper exec of a user binary", command: "/bin/sh -c exec /usr/local/bin/server", want: false},
		{name: "shell without -c", command: "/bin/sh /Applications/Codex.app/Contents/Resources/bin/server", want: false},
		{name: "empty", command: "  ", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, shadowmcp.IsVendorBundleCommand(tc.command))
		})
	}
}
