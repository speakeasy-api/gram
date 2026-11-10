package shadowmcp

import "strings"

// agentBuiltinServerNames lists MCP servers that a coding agent runs inside
// itself rather than loading from user configuration. Hook payloads carry no
// transport or "in-process" marker for these servers: the agent reports the
// tool name alone and the hook relay can only attach a URL or launch command
// when it finds the server in a config file, which a built-in never appears in.
// The server name is therefore the only signal, and it is trusted only when
// provenance was loaded and resolved no URL or command (IsAgentBuiltinServer).
//
// Names are matched exactly. A name a user can also pick for their own server
// (for example any ccd_* name) is listed only once it has been observed as a
// shipped built-in, so an unlisted lookalike stays subject to Shadow MCP.
var agentBuiltinServerNames = map[string]struct{}{
	// Claude in Chrome browser extension, in the Claude Code CLI and the
	// Claude desktop app.
	"claude-in-chrome": {},

	// Claude desktop app built-in browser pane.
	"Claude_Browser": {},

	// Claude desktop app iOS Simulator control.
	"Claude_Code_iOS_Simulator": {},

	// Claude desktop app computer use.
	"computer-use": {},

	// Claude desktop app in-process servers for the turn task list and wrap-up.
	"ccd_turn": {},

	// Claude desktop app in-process servers for starting and handing off
	// sessions.
	"ccd_session": {},

	// Claude desktop app in-process server for listing, archiving and
	// messaging sessions.
	"ccd_session_mgmt": {},

	// Claude desktop app in-process server for pull request status and
	// auto-merge.
	"ccd_pr": {},

	// Claude desktop app in-process server for panes and transcript view.
	"ccd_view": {},

	// Claude desktop app in-process server for window splits and layout.
	"ccd_window": {},

	// Claude desktop app in-process server for sidebar groups and sections.
	"ccd_sidebar": {},

	// Claude desktop app in-process server for app settings.
	"ccd_settings": {},

	// Claude desktop app in-process server for host actions (worktrees,
	// editor, keep awake).
	"ccd_host": {},

	// Claude desktop app in-process server for changing the session directory.
	"ccd_directory": {},

	// Claude desktop app in-process server for session connector status.
	"ccd_connectors": {},
}

// ServerProvenance is what Speakeasy resolved about how an MCP server is
// reached, from a hook payload, a session inventory, or recorded telemetry.
type ServerProvenance struct {
	// Known reports whether the lookup behind URL and Command succeeded. When
	// it is false, an empty URL and Command prove nothing: the server may
	// have a transport that could not be read.
	Known bool

	// URL is the resolved HTTP or SSE endpoint, empty when none was resolved.
	URL string

	// Command is the resolved stdio launch command, or any other identity
	// beyond the server name that pins the server to a configured target.
	// Empty when none was resolved.
	Command string
}

// UnknownServerProvenance is the provenance of a server whose transport could
// not be loaded. It never exempts a server.
func UnknownServerProvenance() ServerProvenance {
	return ServerProvenance{Known: false, URL: "", Command: ""}
}

// IsAgentBuiltinServer reports whether an MCP server is one the coding agent
// ships in-process, which Shadow MCP neither enforces on nor reports.
//
// serverName is the server segment of the agent's tool name
// (mcp__<server>__<tool>). It returns true only when provenance is known and
// resolved neither a URL nor a launch command: unknown provenance fails closed,
// and a server with a URL or a launch command is user configured whatever its
// name, so a local stdio server named like a built-in stays subject to Shadow
// MCP.
func IsAgentBuiltinServer(serverName string, provenance ServerProvenance) bool {
	if !provenance.Known {
		return false
	}
	if strings.TrimSpace(provenance.URL) != "" || strings.TrimSpace(provenance.Command) != "" {
		return false
	}

	_, ok := agentBuiltinServerNames[strings.TrimSpace(serverName)]
	return ok
}
