package hooks

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// gramHostedMCPHosts are the built-in hosts for Gram-managed MCP servers.
// Exact host matching keeps third-party subdomains from being treated as
// trusted Gram-hosted MCP servers.
var gramHostedMCPHosts = []string{
	"app.getgram.ai",
	"chat.speakeasy.com",
}

// parsedClaudeToolName is the result of splitting a Claude Code tool name
// into its MCP "<server>" and "<tool>" parts. IsMCP is false for native
// tools (Read, Edit, Bash, ...) and for malformed mcp__ names.
type parsedClaudeToolName struct {
	Server string
	Tool   string
	IsMCP  bool
}

// parseClaudeToolName splits a Claude Code MCP tool name into its
// "<server>" and "<tool>" parts. Tools follow the "mcp__<server>__<tool>"
// convention; native Claude Code tools (Read, Edit, Bash, ...) return a
// zero-value result with IsMCP=false.
func parseClaudeToolName(rawName string) parsedClaudeToolName {
	// Claude Code uses the "mcp__<server>__<tool>" form. Restrict to that prefix
	// so Cursor's "MCP:" names — which the shared parser also recognizes — don't
	// match here. With the prefix present, AttributeTool only reports isMCP when
	// both the server and tool segments are non-empty.
	if !strings.HasPrefix(rawName, "mcp__") {
		return parsedClaudeToolName{Server: "", Tool: "", IsMCP: false}
	}
	server, tool, isMCP := toolref.AttributeTool(rawName)
	if !isMCP {
		return parsedClaudeToolName{Server: "", Tool: "", IsMCP: false}
	}
	return parsedClaudeToolName{Server: server, Tool: tool, IsMCP: true}
}

// mcpServerPrefix returns the tool-name prefix Claude Code derives for an
// MCP server entry. The rules — inferred from observed tool names like
// "mcp__claude_ai_Linear_Speakeasy__..." against the `claude mcp list`
// entry "claude.ai Linear (Speakeasy)" — are:
//
//   - source "claude.ai" → "claude_ai_" + sanitize(name)
//   - source "plugin"    → "plugin_" + sanitize(plugin) + "_" + sanitize(name)
//   - source "local"     → sanitize(name)
//
// sanitize: spaces become "_", parens are dropped, consecutive "_" are
// collapsed, leading/trailing "_" are trimmed, and hyphens/underscores/
// alphanumerics are preserved. This convention is not documented by
// Claude Code; if it drifts, this function is the only place to update.
func mcpServerPrefix(source, plugin, name string) string {
	switch source {
	case "claude.ai":
		return "claude_ai_" + sanitizeMCPName(name)
	case "plugin":
		return "plugin_" + sanitizeMCPName(plugin) + "_" + sanitizeMCPName(name)
	default:
		return sanitizeMCPName(name)
	}
}

func sanitizeMCPName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch r {
		case ' ':
			b.WriteByte('_')
		case '(', ')':
			// drop
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return strings.Trim(s, "_")
}

// matchCachedMCPEntry returns the cached entry whose derived server prefix
// equals serverPrefix, or nil if none match. For Cowork-shipped entries the
// prefix Claude derives is the connector UUID rather than a sanitized name,
// so we also accept a ConnectorUUID match on the cached entry. Codex entries
// carry a pre-computed ToolPrefix because Codex's sanitizer differs from
// Claude's (every non-alphanumeric/underscore character becomes "_").
func matchCachedMCPEntry(entries []MCPServerEntry, serverPrefix string) *MCPServerEntry {
	var matched *MCPServerEntry
	for i := range entries {
		if !mcpEntryMatchesPrefix(entries[i], serverPrefix) {
			continue
		}
		if matched != nil {
			return nil
		}
		matched = &entries[i]
	}
	return matched
}

func mcpEntryMatchesPrefix(entry MCPServerEntry, serverPrefix string) bool {
	if entry.ToolPrefix != "" && entry.ToolPrefix == serverPrefix {
		return true
	}
	if entry.ConnectorUUID != "" && entry.ConnectorUUID == serverPrefix {
		return true
	}
	return mcpServerPrefix(entry.Source, entry.PluginName, entry.Name) == serverPrefix
}

// matchCodexCachedMCPEntry resolves a raw Codex tool name
// (mcp__<prefix>__<tool>) against the cached inventory. Codex's sanitizer
// preserves consecutive underscores, so the server prefix itself can contain
// "__" (e.g. "foo--bar" sanitizes to "foo__bar") and a naive 3-way split
// truncates it — match by the longest ToolPrefix that prefixes the
// post-mcp__ remainder, falling back to the generic single-prefix match for
// entries without a pre-computed prefix.
func matchCodexCachedMCPEntry(entries []MCPServerEntry, rawToolName string) *MCPServerEntry {
	rest, ok := strings.CutPrefix(rawToolName, "mcp__")
	if !ok {
		return nil
	}
	var best *MCPServerEntry
	ambiguous := false
	for i := range entries {
		p := entries[i].ToolPrefix
		if p == "" || !strings.HasPrefix(rest, p+"__") {
			continue
		}
		if best == nil || len(p) > len(best.ToolPrefix) {
			best = &entries[i]
			ambiguous = false
			continue
		}
		if len(p) == len(best.ToolPrefix) {
			ambiguous = true
		}
	}
	if best != nil {
		if ambiguous {
			return nil
		}
		return best
	}
	return matchCachedMCPEntry(entries, mcpServerIdentityFromToolName(rawToolName))
}

// matchCodexCachedMCPServerEntry resolves the explicit server name Codex
// sends in built-in MCP meta-tool inputs, such as list_mcp_resources. Unlike
// mcp__ tool names, this value is the configured server name rather than a
// sanitized prefix.
func matchCodexCachedMCPServerEntry(entries []MCPServerEntry, serverName string) *MCPServerEntry {
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return nil
	}
	var matched *MCPServerEntry
	for i := range entries {
		if entries[i].Name == serverName {
			if matched != nil {
				return nil
			}
			matched = &entries[i]
		}
	}
	if matched != nil {
		return matched
	}
	if matched := matchCachedMCPEntry(entries, codexSanitizeToolName(serverName)); matched != nil {
		return matched
	}
	return matchCachedMCPEntry(entries, serverName)
}

// applyMCPInventoryAttrs decorates a telemetry attribute map with the
// server URL resolved from the SessionStart MCP inventory and replaces
// the prefix-derived gram.tool_call.source with the human-readable
// server name from the inventory. This unifies the display name across
// both flows: Claude Code's tool prefix is a sanitized form
// ("claude_ai_Slack"), while Cowork's is the connector UUID — both are
// replaced by the inventory's Name ("Slack") so dashboards don't need
// per-flow logic. When the matched entry has no Name (defensive
// fallback) or no entry matched at all, the original prefix-derived
// source is left intact.
func applyMCPInventoryAttrs(attrs map[attr.Key]any, matched *MCPServerEntry) {
	if matched == nil {
		return
	}
	if matched.URL != "" {
		attrs[attr.MCPServerURLKey] = matched.URL
	}
	if matched.Name != "" {
		attrs[attr.ToolCallSourceKey] = matched.Name
	}
}

// resolvedMCPMatch returns the canonical server identifier for a matched
// MCP list entry: the URL for HTTP/SSE servers, the command for stdio
// servers, or — when the snapshot didn't resolve to an entry — the
// server-prefix portion of the tool name as a degraded fallback. Same
// priority used by recordShadowMCPBlockFinding when populating the
// risk_results.match column.
func resolvedMCPMatch(matched *MCPServerEntry, serverPrefix string) string {
	if matched != nil {
		switch {
		case matched.URL != "":
			return matched.URL
		case matched.Command != "":
			return matched.Command
		}
	}
	return serverPrefix
}

// isGramHostedMCPURL reports whether rawURL points at a Gram-managed MCP
// server. Checks the canonical host plus any additional trusted hosts.
// Exact host match, case-insensitive.
func isGramHostedMCPURL(rawURL string, additionalTrustedHosts ...string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	hostname := u.Hostname()
	for _, h := range gramHostedMCPHosts {
		if strings.EqualFold(hostname, h) {
			return true
		}
	}
	for _, h := range additionalTrustedHosts {
		if trustedGramHostedMCPHostMatches(u, h) {
			return true
		}
	}
	return false
}

func trustedGramHostedMCPHostMatches(u *url.URL, trustedHost string) bool {
	trustedHost = strings.TrimSpace(trustedHost)
	if trustedHost == "" {
		return false
	}
	if strings.Contains(trustedHost, "://") {
		parsed, err := url.Parse(trustedHost)
		if err != nil || parsed.Host == "" {
			return false
		}
		trustedHost = parsed.Host
	}
	if strings.Contains(trustedHost, ":") {
		return strings.EqualFold(u.Host, trustedHost)
	}
	return strings.EqualFold(u.Hostname(), trustedHost)
}

// isGramHostedMCPURLForOrg checks if a URL is a Gram-managed MCP server for
// the given organization. It checks against the canonical host first (no DB
// hit), then falls back to checking the org's custom domain if needed.
func (s *Service) isGramHostedMCPURLForOrg(ctx context.Context, rawURL, orgID string) bool {
	trustedHosts := make([]string, 0, 1)
	if s.serverURL != nil && s.serverURL.Host != "" {
		trustedHosts = append(trustedHosts, s.serverURL.Host)
	}

	// Fast path: check built-in/configured hosts before consulting custom domains.
	if isGramHostedMCPURL(rawURL, trustedHosts...) {
		return true
	}
	if rawURL == "" || orgID == "" {
		return false
	}
	customDomain, err := repo.New(s.db).GetCustomDomainByOrganization(ctx, orgID)
	if err != nil || !customDomain.Verified || !customDomain.Activated {
		return false
	}
	return isGramHostedMCPURL(rawURL, customDomain.Domain)
}

// getCachedMCPList retrieves the parsed `claude mcp list` snapshot stored
// at SessionStart. Returns an error when the cache has no entry for the
// session — callers decide whether that means "fall back to allow",
// "buffer", or in the shadow-MCP guard's case, "deny with retry message".
func (s *Service) getCachedMCPList(ctx context.Context, sessionID string) ([]MCPServerEntry, error) {
	var entries []MCPServerEntry
	if err := s.cache.Get(ctx, sessionMCPListCacheKey(sessionID), &entries); err != nil {
		return nil, fmt.Errorf("get cached mcp list: %w", err)
	}
	return entries, nil
}
