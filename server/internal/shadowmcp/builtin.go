package shadowmcp

import "strings"

// agentBuiltinServerNames lists MCP servers that a coding agent runs inside
// itself rather than loading from user configuration. Hook payloads carry no
// transport or "in-process" marker for these servers: the agent reports the
// tool name alone and the hook relay can only attach a URL or launch command
// when it finds the server in a config file, which a built-in never appears in.
// The server name is therefore the only signal, and it is trusted only when no
// URL or command was resolved (IsAgentBuiltinServer).
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
}

// agentBuiltinServerPrefixes lists name prefixes reserved by a coding agent
// for its own in-process MCP servers.
var agentBuiltinServerPrefixes = []string{
	// Claude desktop app in-process servers (ccd_turn, ccd_session, ccd_pr,
	// and the other ccd_* task and window tools).
	"ccd_",
}

// IsAgentBuiltinServer reports whether an MCP server is one the coding agent
// ships in-process, which Shadow MCP neither enforces on nor reports.
//
// serverName is the server segment of the agent's tool name
// (mcp__<server>__<tool>). url and command are whatever the hook payload, the
// session inventory, or recorded provenance resolved for that server. A server
// with a URL or a launch command is user configured, whatever its name, so a
// local stdio server named like a built-in stays subject to Shadow MCP.
func IsAgentBuiltinServer(serverName string, url string, command string) bool {
	if strings.TrimSpace(url) != "" || strings.TrimSpace(command) != "" {
		return false
	}

	name := strings.TrimSpace(serverName)
	if _, ok := agentBuiltinServerNames[name]; ok {
		return true
	}
	for _, prefix := range agentBuiltinServerPrefixes {
		if len(name) > len(prefix) && strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
