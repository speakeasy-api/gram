package shadowmcp

import (
	"regexp"
	"strings"
)

// agentBuiltinServerNames lists MCP servers that a coding agent is known to
// run inside itself rather than load from user configuration. The list only
// labels why a server was treated as a built-in (BuiltinEvidenceKnownName):
// ClassifyServer treats a server with no URL, no launch command and no install
// evidence as a built-in whether or not its name is listed here.
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

// IsKnownAgentBuiltinServerName reports whether serverName is on the list of
// MCP servers a coding agent is known to ship in-process. Names match exactly.
func IsKnownAgentBuiltinServerName(serverName string) bool {
	_, ok := agentBuiltinServerNames[strings.TrimSpace(serverName)]
	return ok
}

// ServerKind is how Shadow MCP treats the MCP server a tool call targets.
type ServerKind string

const (
	// ServerKindURL is a server whose HTTP or SSE URL is known. It is judged
	// by that URL: Speakeasy-hosted or approved servers are allowed, anything
	// else is Shadow MCP.
	ServerKindURL ServerKind = "url"

	// ServerKindConnector is a Claude desktop or Cowork connector, named by
	// its connector UUID, whose URL is not known. It is Shadow MCP and is
	// treated as not Speakeasy-hosted.
	ServerKindConnector ServerKind = "connector"

	// ServerKindCommand is a local server started by a launch command the
	// user configured. It is Shadow MCP.
	ServerKindCommand ServerKind = "command"

	// ServerKindInstalled is a server with no known URL or launch command
	// that the user's configuration lists. It is Shadow MCP.
	ServerKindInstalled ServerKind = "installed"

	// ServerKindUnidentified is a call whose server name could not be read.
	// It is Shadow MCP: an unknown target is never exempt.
	ServerKindUnidentified ServerKind = "unidentified"

	// ServerKindBuiltin is a coding agent's own built-in server. Shadow MCP
	// never blocks it and never reports it as a finding.
	ServerKindBuiltin ServerKind = "builtin"
)

// BuiltinEvidence records why a server was classified as a coding agent
// built-in. It is stored on hook telemetry rows as gram.mcp.builtin_evidence.
type BuiltinEvidence string

const (
	// BuiltinEvidenceNone marks a classification that is not a built-in.
	BuiltinEvidenceNone BuiltinEvidence = ""

	// BuiltinEvidenceVendorBundle marks a server whose launch command runs an
	// executable shipped inside a coding agent's own application bundle or
	// managed directory.
	BuiltinEvidenceVendorBundle BuiltinEvidence = "vendor_bundle"

	// BuiltinEvidenceCursorBuiltin marks a Cursor server whose Cursor server
	// identifier carries Cursor's own "cursor-" prefix.
	BuiltinEvidenceCursorBuiltin BuiltinEvidence = "cursor_builtin"

	// BuiltinEvidenceKnownName marks a server on the known built-in name list.
	BuiltinEvidenceKnownName BuiltinEvidence = "known_name"

	// BuiltinEvidenceNotInInventory marks an unlisted server name that a
	// complete MCP inventory for the session or device does not configure.
	BuiltinEvidenceNotInInventory BuiltinEvidence = "not_in_inventory"

	// BuiltinEvidenceUnverified marks an unlisted server name for which no
	// MCP inventory could be loaded. It is still allowed.
	BuiltinEvidenceUnverified BuiltinEvidence = "unverified"
)

// ParseBuiltinEvidence returns the BuiltinEvidence a recorded attribute value
// names, and false for any value ClassifyServer never records.
func ParseBuiltinEvidence(value string) (BuiltinEvidence, bool) {
	switch evidence := BuiltinEvidence(strings.TrimSpace(value)); evidence {
	case BuiltinEvidenceVendorBundle, BuiltinEvidenceCursorBuiltin, BuiltinEvidenceKnownName, BuiltinEvidenceNotInInventory, BuiltinEvidenceUnverified:
		return evidence, true
	default:
		return BuiltinEvidenceNone, false
	}
}

// ServerFacts is what Speakeasy knows about the MCP server a tool call
// targets, gathered from the hook payload, the session MCP inventory, and the
// device MCP inventory.
type ServerFacts struct {
	// Name is the server segment of the tool name (mcp__<server>__<tool>), or
	// the server name the agent reported when the tool name has none.
	Name string

	// URL is the server's HTTP or SSE endpoint, empty when unknown.
	URL string

	// Command is the server's stdio launch command, empty when unknown.
	Command string

	// Installed reports evidence that the user installed the server: a
	// loaded MCP inventory lists it under Name.
	Installed bool

	// InventoryLoaded reports whether a complete MCP inventory (session or
	// device) was consulted. It only selects between the not_in_inventory and
	// unverified evidence labels.
	InventoryLoaded bool

	// CursorServerIdentifier is the Cursor server identifier the relay read
	// from Cursor's per-project MCP metadata, empty for other agents.
	// "cursor-" identifiers are Cursor built-ins; "plugin-" and "user-"
	// identifiers are servers the user installed.
	CursorServerIdentifier string
}

// Classification is the outcome of ClassifyServer.
type Classification struct {
	// Kind is how Shadow MCP treats the server.
	Kind ServerKind

	// Evidence is why the server is a built-in. It is BuiltinEvidenceNone
	// unless Kind is ServerKindBuiltin.
	Evidence BuiltinEvidence
}

// IsBuiltin reports whether the server is a coding agent built-in, which
// Shadow MCP neither enforces on nor reports.
func (c Classification) IsBuiltin() bool {
	return c.Kind == ServerKindBuiltin
}

// ClassifyServer decides how Shadow MCP treats an MCP server. The rules apply
// in order:
//
//  1. A known URL decides: the caller judges it as Speakeasy-hosted, approved,
//     or Shadow MCP.
//  2. A server named by a UUID is a Claude desktop or Cowork connector whose
//     URL is unknown: Shadow MCP.
//  3. A known launch command is a user-installed local server, Shadow MCP,
//     unless its executable lives inside a coding agent's own bundle.
//  4. Install evidence without a URL or command is Shadow MCP.
//  5. Anything else is a built-in.
//
// Rule 5 accepts missing some Shadow MCP servers in exchange for never
// blocking or reporting a coding agent's own servers, including built-ins
// that are not on any list yet.
func ClassifyServer(facts ServerFacts) Classification {
	name := strings.TrimSpace(facts.Name)
	if strings.TrimSpace(facts.URL) != "" {
		return Classification{Kind: ServerKindURL, Evidence: BuiltinEvidenceNone}
	}
	if IsConnectorUUID(name) {
		return Classification{Kind: ServerKindConnector, Evidence: BuiltinEvidenceNone}
	}
	if command := strings.TrimSpace(facts.Command); command != "" {
		if IsVendorBundleCommand(command) {
			return Classification{Kind: ServerKindBuiltin, Evidence: BuiltinEvidenceVendorBundle}
		}
		return Classification{Kind: ServerKindCommand, Evidence: BuiltinEvidenceNone}
	}
	if name == "" {
		return Classification{Kind: ServerKindUnidentified, Evidence: BuiltinEvidenceNone}
	}
	cursorIdentifier := strings.TrimSpace(facts.CursorServerIdentifier)
	if facts.Installed || IsCursorUserInstalledIdentifier(cursorIdentifier) {
		return Classification{Kind: ServerKindInstalled, Evidence: BuiltinEvidenceNone}
	}

	evidence := BuiltinEvidenceUnverified
	switch {
	case IsCursorBuiltinIdentifier(cursorIdentifier):
		evidence = BuiltinEvidenceCursorBuiltin
	case IsKnownAgentBuiltinServerName(name):
		evidence = BuiltinEvidenceKnownName
	case facts.InventoryLoaded:
		evidence = BuiltinEvidenceNotInInventory
	}
	return Classification{Kind: ServerKindBuiltin, Evidence: evidence}
}

// connectorUUIDPattern matches the canonical 8-4-4-4-12 hexadecimal form the
// Claude desktop app and Cowork use as a connector's MCP server name.
var connectorUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsConnectorUUID reports whether an MCP server name is a connector UUID.
func IsConnectorUUID(serverName string) bool {
	return connectorUUIDPattern.MatchString(strings.TrimSpace(serverName))
}

const (
	// cursorBuiltinIdentifierPrefix starts the server identifier of servers
	// Cursor ships itself (cursor-ide-browser, cursor-app-control).
	cursorBuiltinIdentifierPrefix = "cursor-"

	// cursorPluginIdentifierPrefix starts the server identifier of servers a
	// Cursor plugin the user installed provides.
	cursorPluginIdentifierPrefix = "plugin-"

	// cursorUserIdentifierPrefix starts the server identifier of servers the
	// user configured or an editor extension the user installed provides.
	cursorUserIdentifierPrefix = "user-"
)

// IsCursorBuiltinIdentifier reports whether a Cursor server identifier names a
// server Cursor ships itself.
func IsCursorBuiltinIdentifier(identifier string) bool {
	return hasNonEmptySuffix(strings.TrimSpace(identifier), cursorBuiltinIdentifierPrefix)
}

// IsCursorUserInstalledIdentifier reports whether a Cursor server identifier
// names a server the user installed, directly or through a plugin.
func IsCursorUserInstalledIdentifier(identifier string) bool {
	identifier = strings.TrimSpace(identifier)
	return hasNonEmptySuffix(identifier, cursorPluginIdentifierPrefix) || hasNonEmptySuffix(identifier, cursorUserIdentifierPrefix)
}

// IsCursorServerIdentifier reports whether a value has the shape of a Cursor
// server identifier.
func IsCursorServerIdentifier(identifier string) bool {
	return IsCursorBuiltinIdentifier(identifier) || IsCursorUserInstalledIdentifier(identifier)
}

func hasNonEmptySuffix(value, prefix string) bool {
	return len(value) > len(prefix) && strings.HasPrefix(value, prefix)
}
