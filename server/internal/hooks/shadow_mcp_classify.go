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
// consults: the session's own snapshot, or the device copy when the session
// has none yet.
type shadowMCPInventory struct {
	// entries is the inventory, empty when none could be loaded.
	entries []MCPServerEntry

	// loaded reports whether a complete inventory was found.
	loaded bool
}

// inventoryServerFacts reports what a loaded MCP inventory says about the
// server a tool call names: whether it lists the server, and the URL or launch
// command of the entry that does. Several entries that disagree on transport
// count as listed with none, so the server is Shadow MCP without a URL that
// could clear it.
func inventoryServerFacts(entries []MCPServerEntry, serverName string) (string, string, bool) {
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
	if matched == nil {
		return "", "", false
	}
	if conflicting {
		return "", "", true
	}
	return strings.TrimSpace(matched.URL), strings.TrimSpace(matched.Command), true
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
func inventoryShadowMCPFacts(serverName string, inventory shadowMCPInventory) shadowmcp.ServerFacts {
	facts := shadowmcp.ServerFacts{
		Name:                   serverName,
		URL:                    "",
		Command:                "",
		Installed:              false,
		InventoryLoaded:        inventory.loaded,
		CursorServerIdentifier: "",
	}
	if inventory.loaded {
		facts.URL, facts.Command, facts.Installed = inventoryServerFacts(inventory.entries, serverName)
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
		if inventory.loaded {
			facts.InventoryLoaded = true
			facts.URL, facts.Command, facts.Installed = inventoryServerFacts(inventory.entries, serverName)
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
	}
	target.classification = shadowmcp.ClassifyServer(facts)
	return target
}

// loadShadowMCPInventory returns the session's MCP inventory snapshot, falling
// back to the device copy for the scope deviceScope builds when the session
// has none.
func (s *Service) loadShadowMCPInventory(ctx context.Context, sessionID string, deviceScope func() deviceMCPListScope) shadowMCPInventory {
	if sessionID != "" {
		if entries, err := s.getCachedMCPList(ctx, sessionID); err == nil {
			return shadowMCPInventory{entries: entries, loaded: true}
		}
	}
	if entries, ok := s.getDeviceMCPList(ctx, deviceScope()); ok {
		return shadowMCPInventory{entries: entries, loaded: true}
	}
	return shadowMCPInventory{entries: nil, loaded: false}
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
// the OTEL stream fills in; the hostname rides on every event.
func (s *Service) canonicalDeviceMCPListScope(ctx context.Context, authCtx *contextvalues.AuthContext, userID string, payload *gen.IngestPayload) deviceMCPListScope {
	scope := deviceMCPListScope{
		ProjectID: "",
		UserID:    strings.TrimSpace(userID),
		DeviceID:  "",
		Hostname:  "",
		Adapter:   "",
		Cwd:       canonicalCwd(payload),
	}
	if authCtx == nil || authCtx.ProjectID == nil || payload == nil || payload.Source == nil {
		return scope
	}
	scope.ProjectID = authCtx.ProjectID.String()
	scope.Hostname = strings.TrimSpace(conv.PtrValOr(payload.Source.Hostname, ""))
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
