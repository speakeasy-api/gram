package hooks

import (
	"context"
	"fmt"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

func (s *Service) enforceShadowMCPToolAccess(
	ctx context.Context,
	organizationID string,
	projectID string,
	userID string,
	policy *risk.ShadowMCPPolicy,
	toolName string,
	evidence shadowmcp.AccessEvidence,
) (string, bool) {
	var detail string
	switch {
	case evidence.FullURL != "":
		if s.shadowMCPClient.IsGramHostedMCPURLForOrg(ctx, evidence.FullURL, organizationID) {
			return "", false
		}
		detail = fmt.Sprintf("MCP server is not Gram-hosted (URL: %s)", evidence.FullURL)
	default:
		detail = "MCP server is not Gram-hosted"
	}

	// Allow-all policies invert the default: every non-Gram-hosted server is
	// permitted unless its URL is on the policy's blocked list. Bypass grants
	// are a block_all concept and are never consulted here — the blocked-list
	// membership is the whole check. Evidence without a resolvable URL (e.g. a
	// local stdio server) cannot match a URL blocklist and falls through to an
	// allow.
	if policy.IsAllowAll() {
		blockedURL, blocked := shadowmcp.BlockedURLMatch(policy.BlockedURLs, evidence.FullURL)
		if !blocked {
			return "", false
		}
		s.logger.InfoContext(ctx, "shadow-mcp call blocked by allow-all policy blocked list",
			attr.SlogEvent("shadow_mcp_policy_blocklist_deny"),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID),
			attr.SlogRiskPolicyID(policy.ID),
			attr.SlogValueAny(map[string]any{
				"blocked_url": blockedURL,
				"tool_name":   toolName,
			}),
		)
		return fmt.Sprintf("MCP server is blocked by policy (URL: %s)", blockedURL), true
	}

	if target, allowed := s.canBypassPolicy(ctx, organizationID, userID, policy.ID, evidence, toolName); allowed {
		s.logger.InfoContext(ctx, "shadow-mcp call allowed via risk policy bypass grant",
			attr.SlogEvent("shadow_mcp_policy_bypass_allow"),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID),
			attr.SlogRiskPolicyID(policy.ID),
			attr.SlogValueAny(map[string]any{
				"target_kind": target.Kind,
				"target_key":  target.Key,
				"tool_name":   toolName,
			}),
		)
		return "", false
	}
	return detail, true
}

func (s *Service) canBypassPolicy(
	ctx context.Context,
	organizationID string,
	userID string,
	policyID string,
	evidence shadowmcp.AccessEvidence,
	toolName string,
) (*risk.PolicyBypassTarget, bool) {
	target := risk.ShadowMCPPolicyBypassTarget(evidence, toolName)
	if target == nil {
		return nil, false
	}
	allowed := s.policyBypass.CanBypass(ctx, risk.PolicyBypassEvaluation{
		OrganizationID: organizationID,
		UserID:         userID,
		PolicyID:       policyID,
		Target:         target,
	})
	if !allowed {
		return nil, false
	}
	return target, true
}

// codexShadowMCPEvidence builds the access evidence for a Codex shadow-MCP
// tool call and returns the inventory entry it resolved to, if any. The
// tool-name prefix gives the server identity; when the SessionStart
// inventory snapshot resolves that prefix to a configured server, its URL is
// attached so bypass grants and access requests can be scoped to the server
// URL rather than just the name.
func (s *Service) codexShadowMCPEvidence(ctx context.Context, payload *gen.CodexPayload) (shadowmcp.AccessEvidence, *MCPServerEntry) {
	rawToolName := conv.PtrValOr(payload.ToolName, "")
	serverIdentity := mcpServerIdentityFromToolName(rawToolName)
	if serverIdentity == "" {
		if metaToolServer, ok := codexMCPMetaToolServer(payload); ok {
			serverIdentity = metaToolServer
		}
	}
	evidence := shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: serverIdentity,
	}
	sessionID := conv.PtrValOr(payload.SessionID, "")
	if evidence.ServerIdentity == "" || sessionID == "" {
		return evidence, nil
	}
	entries, err := s.getCachedMCPList(ctx, sessionID)
	if err != nil {
		return evidence, nil
	}
	var matched *MCPServerEntry
	if strings.HasPrefix(rawToolName, "mcp__") {
		matched = matchCodexCachedMCPEntry(entries, rawToolName)
	} else {
		matched = matchCodexCachedMCPServerEntry(entries, evidence.ServerIdentity)
	}
	if matched != nil {
		if matched.ToolPrefix != "" && strings.HasPrefix(rawToolName, "mcp__") {
			// The naive 3-way split truncates prefixes containing "__"; the
			// matched entry carries the full sanitized prefix.
			evidence.ServerIdentity = matched.ToolPrefix
		}
		applyMCPEntryToEvidence(&evidence, matched)
	}
	return evidence, matched
}

// applyMCPEntryToEvidence upgrades evidence to the target a matched inventory
// entry actually routes to. Shared by the legacy Codex endpoint and the
// canonical ingest guard so the two cannot drift on what a resolved entry
// means — the guard's allow/deny turns on exactly this mapping.
func applyMCPEntryToEvidence(evidence *shadowmcp.AccessEvidence, matched *MCPServerEntry) {
	if matched == nil {
		return
	}
	if matched.URL != "" {
		evidence.FullURL = matched.URL
	}
	if matched.Command != "" {
		// Pin stdio identity to the launch command, mirroring the Claude
		// guard — a bypass grant must not follow a renamed config alias.
		evidence.ServerIdentity = matched.Command
	}
}

func codexMCPMetaToolServer(payload *gen.CodexPayload) (string, bool) {
	if payload == nil {
		return "", false
	}
	return codexMetaToolServer(conv.PtrValOr(payload.ToolName, ""), payload.ToolInput)
}

// codexMetaToolServer reports whether a tool call is one of Codex's built-in
// MCP resource tools and, if so, the server it targets. These are the only MCP
// calls Codex makes that carry no mcp__ prefix — the target lives in
// tool_input.server instead — so every gate that decides "is this an MCP call"
// has to ask here as well as checking the tool name. An unreadable input still
// reports true with an empty server: it is an MCP call whose target could not
// be established, which callers must treat as unproven rather than absent.
func codexMetaToolServer(toolName string, toolInput any) (string, bool) {
	switch toolName {
	case "list_mcp_resources", "list_mcp_resource_templates", "read_mcp_resource":
	default:
		return "", false
	}
	input, ok := toolInput.(map[string]any)
	if !ok {
		return "", true
	}
	server, _ := input["server"].(string)
	return strings.TrimSpace(server), true
}

// codexInventoryProvenanceDetail reports why a Codex MCP call should be
// denied based on where the SessionStart inventory says the matched server
// actually routes: an external (non-Gram) URL or a local stdio server. An
// empty string means the inventory raises no objection — either the entry is
// Gram-hosted or there is nothing to cross-check (nil entry). Mirrors the
// target checks of the Claude PreToolUse guard.
func (s *Service) codexInventoryProvenanceDetail(ctx context.Context, matched *MCPServerEntry, orgID string) string {
	if matched == nil {
		return ""
	}
	switch {
	case matched.URL != "" && !s.shadowMCPClient.IsGramHostedMCPURLForOrg(ctx, matched.URL, orgID):
		return fmt.Sprintf("MCP server %q is not Gram-hosted (URL: %s)", matched.Name, matched.URL)
	case matched.URL == "" && matched.Command != "":
		return fmt.Sprintf("MCP server %q is a local stdio server (command: %s)", matched.Name, matched.Command)
	default:
		return ""
	}
}

func cursorShadowMCPEvidence(payload *gen.CursorPayload) shadowmcp.AccessEvidence {
	return shadowmcp.AccessEvidence{
		FullURL:        conv.PtrValOr(payload.URL, ""),
		URLHost:        "",
		ServerIdentity: cursorMCPToolSource(payload),
	}
}

func claudeShadowMCPEvidence(rawToolName string) shadowmcp.AccessEvidence {
	return shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: mcpServerIdentityFromToolName(rawToolName),
	}
}

// mcpServerIdentityFromToolName extracts the MCP server identity from a
// Claude-style mcp__ tool name. Some Codex MCP calls arrive as built-in
// meta-tools with the server name in tool_input.server; those are handled by
// codexMCPMetaToolServer.
func mcpServerIdentityFromToolName(rawName string) string {
	return toolref.MCPServerOf(rawName)
}
