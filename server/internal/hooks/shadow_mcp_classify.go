package hooks

import (
	"context"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// cursorAdapter is the hook adapter slug of the Cursor relay, the only sender
// that reports a Cursor server identifier in data.mcp.server_identity.
const cursorAdapter = "cursor"

// shadowMCPInventory is the MCP inventory a Shadow MCP classification
// consults. The complete list is the session's own snapshot, the only list
// that proves a server absent. Without one, two lists stand in as install
// evidence only: the partial list the sender read before its complete list
// was ready, and the device copy of the last complete list, which can be
// days old and miss servers installed since.
type shadowMCPInventory struct {
	// entries is the session's complete inventory, empty when none loaded.
	entries []MCPServerEntry

	// loaded reports whether the session's complete inventory was found.
	loaded bool

	// partial is the session's partial inventory, consulted only when the
	// session's own snapshot did not load.
	partial []MCPServerEntry

	// device is the device copy of the last complete inventory, consulted
	// after the partial list and only when the session's own snapshot did not
	// load.
	device []MCPServerEntry
}

// find returns the inventory entry that names the server a tool call targets
// and whether any entry does. The session's complete list decides alone when
// it loaded; otherwise the partial list, then the device copy, are searched,
// and a server missing from both is reported unlisted.
func (inventory shadowMCPInventory) find(serverName, rawToolName string) (*MCPServerEntry, bool) {
	if inventory.loaded {
		return findInventoryServer(inventory.entries, serverName, rawToolName)
	}
	if matched, listed := findInventoryServer(inventory.partial, serverName, rawToolName); listed {
		return matched, true
	}
	return findInventoryServer(inventory.device, serverName, rawToolName)
}

// findInventoryServer returns the inventory entry that names the server a
// tool call targets and whether any entry does. Several entries that disagree
// on transport count as listed with no entry, so the server is Shadow MCP
// without a URL that could clear it. A Codex tool prefix can itself contain
// "__", which truncates the server segment of the tool name, so the longest
// configured prefix that starts the tool name also names the server.
func findInventoryServer(entries []MCPServerEntry, serverName, rawToolName string) (*MCPServerEntry, bool) {
	var matched *MCPServerEntry
	conflicting := false
	for i := range entries {
		if !inventoryEntryNamesServer(entries[i], serverName) {
			continue
		}
		if matched == nil {
			matched = &entries[i]
			continue
		}
		if matched.URL != entries[i].URL || matched.Command != entries[i].Command {
			conflicting = true
		}
	}
	if conflicting {
		return nil, true
	}
	if matched != nil {
		return matched, true
	}
	if byPrefix := matchCodexCachedMCPEntry(entries, rawToolName); byPrefix != nil && byPrefix.ToolPrefix != "" {
		return byPrefix, true
	}
	return nil, false
}

// inventoryEntryNamesServer reports whether an inventory entry is the server a
// tool call names. Besides the exact name, the tool-name prefix the agent
// derives, and the connector UUID, it accepts a loose match that ignores case
// and punctuation, because each agent sanitizes server names into tool names
// differently ("plugin:slack:slack" becomes "plugin_slack_slack"). A looser
// match only adds install evidence, which makes a call Shadow MCP rather than
// a built-in.
func inventoryEntryNamesServer(entry MCPServerEntry, serverName string) bool {
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return false
	}
	if entry.Name == serverName || mcpEntryMatchesPrefix(entry, serverName) {
		return true
	}
	key := looseMCPServerKey(serverName)
	if key == "" {
		return false
	}
	return looseMCPServerKey(entry.Name) == key || looseMCPServerKey(entry.ToolPrefix) == key
}

// looseMCPServerKey lowercases a server name and folds every run of
// characters other than letters and digits into a single underscore.
func looseMCPServerKey(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	pendingSeparator := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingSeparator && b.Len() > 0 {
				b.WriteByte('_')
			}
			pendingSeparator = false
			b.WriteRune(r)
			continue
		}
		pendingSeparator = true
	}
	return b.String()
}

// inventoryShadowMCPFacts builds the classification facts for a server from an
// inventory alone, for senders whose payload states no URL or command.
func inventoryShadowMCPFacts(serverName, rawToolName string, inventory shadowMCPInventory) shadowmcp.ServerFacts {
	facts := shadowmcp.ServerFacts{
		Name:                   serverName,
		URL:                    "",
		Command:                "",
		Installed:              false,
		InventoryLoaded:        inventory.loaded,
		CursorServerIdentifier: "",
	}
	matched, listed := inventory.find(serverName, rawToolName)
	facts.Installed = listed
	if matched != nil {
		facts.URL = strings.TrimSpace(matched.URL)
		facts.Command = strings.TrimSpace(matched.Command)
	}
	return facts
}

// canonicalShadowMCPTarget is the classified MCP server a canonical tool call
// targets.
type canonicalShadowMCPTarget struct {
	// classification is how Shadow MCP treats the server.
	classification shadowmcp.Classification

	// evidence is the call's access evidence, upgraded with the URL or launch
	// command the inventory resolved when the payload stated neither.
	evidence shadowmcp.AccessEvidence

	// inventoryURL is the URL the inventory resolved, empty when the payload
	// stated the transport or the inventory resolved none.
	inventoryURL string

	// inventoryCommand is the launch command the inventory resolved, empty
	// when the payload stated the transport or the inventory resolved none.
	inventoryCommand string
}

// classifyCanonicalShadowMCP classifies the MCP server a canonical tool call
// targets. The payload's URL or launch command decides first; when it states
// neither, the session MCP inventory, or the device copy when the session has
// none yet, supplies the transport and the install evidence.
//
// deviceScope is called only when the device copy is needed, since building
// it reads the session's cached metadata.
func (s *Service) classifyCanonicalShadowMCP(ctx context.Context, payload *gen.IngestPayload, rawToolName string, toolInput any, deviceScope func() deviceMCPListScope) canonicalShadowMCPTarget {
	serverName := canonicalShadowMCPServerName(payload, rawToolName, toolInput)
	evidence := canonicalShadowMCPEvidence(payload, rawToolName)
	if evidence.ServerIdentity == "" && evidence.FullURL == "" {
		evidence.ServerIdentity = serverName
	}
	facts := canonicalPayloadServerFacts(payload, serverName, evidence)
	target := canonicalShadowMCPTarget{
		classification:   shadowmcp.Classification{Kind: shadowmcp.ServerKindUnidentified, Evidence: shadowmcp.BuiltinEvidenceNone},
		evidence:         evidence,
		inventoryURL:     "",
		inventoryCommand: "",
	}

	if facts.URL == "" && facts.Command == "" && serverName != "" {
		inventory := s.loadShadowMCPInventory(ctx, canonicalSessionID(payload), deviceScope)
		inventoryFacts := inventoryShadowMCPFacts(serverName, rawToolName, inventory)
		facts.URL = inventoryFacts.URL
		facts.Command = inventoryFacts.Command
		facts.Installed = inventoryFacts.Installed
		facts.InventoryLoaded = inventoryFacts.InventoryLoaded
		target.inventoryURL = facts.URL
		target.inventoryCommand = facts.Command
		if facts.URL != "" {
			target.evidence.FullURL = facts.URL
		}
		if facts.Command != "" {
			// Pin stdio identity to the launch command, as the inventory
			// resolution of the legacy endpoints does, so a bypass grant
			// cannot follow a renamed config alias.
			target.evidence.ServerIdentity = facts.Command
		}
	}
	target.classification = shadowmcp.ClassifyServer(facts)
	return target
}

// loadShadowMCPInventory returns the MCP inventory to classify a session's
// tool call against. The session's own snapshot wins outright. Without one,
// the session's partial list and the device copy for the scope deviceScope
// builds are loaded as install evidence only.
func (s *Service) loadShadowMCPInventory(ctx context.Context, sessionID string, deviceScope func() deviceMCPListScope) shadowMCPInventory {
	inventory := shadowMCPInventory{entries: nil, loaded: false, partial: nil, device: nil}
	if sessionID != "" {
		if entries, err := s.getCachedMCPList(ctx, sessionID); err == nil {
			inventory.entries = entries
			inventory.loaded = true
			return inventory
		}
		if partial, err := s.getCachedPartialMCPList(ctx, sessionID); err == nil {
			inventory.partial = partial
		}
	}
	if entries, ok := s.getDeviceMCPList(ctx, deviceScope()); ok {
		inventory.device = entries
	}
	return inventory
}

// canonicalShadowMCPServerName is the server a canonical MCP tool call
// targets: the server segment of an mcp__<server>__<tool> name, else the
// server the agent reported, else the server a Codex meta-tool names in its
// input. Cursor's "MCP:<tool>" names carry no server, so without a reported
// server name a Cursor server identifier stands in. An empty result means the
// target is unknown.
func canonicalShadowMCPServerName(payload *gen.IngestPayload, rawToolName string, toolInput any) string {
	if strings.HasPrefix(rawToolName, "mcp__") {
		return toolref.MCPServerOf(rawToolName)
	}
	if mcp := canonicalMCPData(payload); mcp != nil {
		if name := strings.TrimSpace(conv.PtrValOr(mcp.ServerName, "")); name != "" {
			return name
		}
	}
	if server, isMetaTool := codexMetaToolServer(rawToolName, toolInput); isMetaTool {
		return server
	}
	return canonicalCursorServerIdentifier(payload)
}

// canonicalCursorServerIdentifier is the Cursor server identifier the Cursor
// relay reports as data.mcp.server_identity, empty for other senders and for
// identities of any other shape.
func canonicalCursorServerIdentifier(payload *gen.IngestPayload) string {
	if payload == nil || payload.Source == nil || !strings.EqualFold(strings.TrimSpace(payload.Source.Adapter), cursorAdapter) {
		return ""
	}
	mcp := canonicalMCPData(payload)
	if mcp == nil {
		return ""
	}
	identity := strings.TrimSpace(conv.PtrValOr(mcp.ServerIdentity, ""))
	if !shadowmcp.IsCursorServerIdentifier(identity) {
		return ""
	}
	return identity
}

// canonicalPayloadServerFacts is what a canonical hook payload itself states
// about its MCP server. A server identity that is neither the server's name
// nor a Cursor server identifier counts as a launch command: the relay copies
// a stdio command there, so an identity of any other shape is never read as
// proof that the server has no transport.
func canonicalPayloadServerFacts(payload *gen.IngestPayload, serverName string, evidence shadowmcp.AccessEvidence) shadowmcp.ServerFacts {
	command := ""
	payloadServerName := ""
	identity := ""
	if mcp := canonicalMCPData(payload); mcp != nil {
		command = strings.TrimSpace(conv.PtrValOr(mcp.Command, ""))
		payloadServerName = strings.TrimSpace(conv.PtrValOr(mcp.ServerName, ""))
		identity = strings.TrimSpace(conv.PtrValOr(mcp.ServerIdentity, ""))
	}

	cursorIdentifier := canonicalCursorServerIdentifier(payload)
	if command == "" && identity != "" && identity != serverName && identity != payloadServerName && identity != cursorIdentifier {
		command = identity
	}
	return shadowmcp.ServerFacts{
		Name:                   serverName,
		URL:                    strings.TrimSpace(evidence.FullURL),
		Command:                command,
		Installed:              false,
		InventoryLoaded:        false,
		CursorServerIdentifier: cursorIdentifier,
	}
}

// canonicalDeviceMCPListScope is the device MCP inventory scope of a canonical
// hook event. The device id comes from the session's cached metadata, which
// the OTEL stream fills in.
func (s *Service) canonicalDeviceMCPListScope(ctx context.Context, authCtx *contextvalues.AuthContext, userID string, payload *gen.IngestPayload) deviceMCPListScope {
	scope := deviceMCPListScope{
		ProjectID: "",
		UserID:    strings.TrimSpace(userID),
		DeviceID:  "",
		Adapter:   "",
		Cwd:       canonicalCwd(payload),
	}
	if authCtx == nil || authCtx.ProjectID == nil || payload == nil || payload.Source == nil {
		return scope
	}
	scope.ProjectID = authCtx.ProjectID.String()
	scope.Adapter = strings.TrimSpace(payload.Source.Adapter)

	sessionID := canonicalSessionID(payload)
	if sessionID == "" || isAgentActor(ctx) {
		return scope
	}
	cached, err := s.getSessionMetadata(ctx, sessionID)
	if err != nil || cached.GramOrgID != authCtx.ActiveOrganizationID || cached.ProjectID != scope.ProjectID {
		return scope
	}
	scope.DeviceID = strings.TrimSpace(cached.DeviceID)
	scope.Cwd = conv.Default(scope.Cwd, strings.TrimSpace(cached.Cwd))
	return scope
}

// applyCanonicalShadowMCPAttrs records how Shadow MCP classified a canonical
// MCP tool event's server on its telemetry row: the URL or launch command an
// inventory resolved when the payload stated neither, and the built-in
// evidence when the server is a coding agent built-in. The offline findings
// scanner reads both back.
func (s *Service) applyCanonicalShadowMCPAttrs(ctx context.Context, attrs map[attr.Key]any, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, userID string) {
	rawToolName := canonicalToolName(payload)
	if !toolref.IsMCPToolName(rawToolName) {
		return
	}
	target := s.classifyCanonicalShadowMCP(ctx, payload, rawToolName, nil, func() deviceMCPListScope {
		return s.canonicalDeviceMCPListScope(ctx, authCtx, userID, payload)
	})
	if _, matched := attrs[attr.MCPMatchKey]; !matched {
		switch {
		case target.inventoryURL != "":
			attrs[attr.MCPServerURLKey] = target.inventoryURL
			attrs[attr.MCPMatchKey] = target.inventoryURL
		case target.inventoryCommand != "":
			attrs[attr.MCPMatchKey] = target.inventoryCommand
		}
	}
	if target.classification.IsBuiltin() {
		attrs[attr.MCPBuiltinEvidenceKey] = string(target.classification.Evidence)
	}
}
