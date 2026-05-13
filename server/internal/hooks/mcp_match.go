package hooks

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// gramHostedMCPHost is the canonical host for Gram-managed MCP servers.
// The shadow-MCP guard allows a tool call only when the cached server
// entry's URL exactly matches this host (case-insensitive). We avoid a
// suffix-match on ".getgram.ai" because a third party squatting on a
// subdomain (e.g. via a CNAME mistake) could bypass the guard otherwise.
const gramHostedMCPHost = "app.getgram.ai"

// claudeMCPServerAndTool splits a Claude Code MCP tool name into its
// "<server>" and "<tool>" parts. Tools follow the "mcp__<server>__<tool>"
// convention; native Claude Code tools (Read, Edit, Bash, ...) return
// ("", "", false).
func claudeMCPServerAndTool(rawName string) (string, string, bool) {
	if !strings.HasPrefix(rawName, "mcp__") {
		return "", "", false
	}
	parts := strings.SplitN(rawName, "__", 3)
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
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
		switch {
		case r == ' ':
			b.WriteByte('_')
		case r == '(' || r == ')':
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
// equals serverPrefix, or nil if none match.
func matchCachedMCPEntry(entries []MCPServerEntry, serverPrefix string) *MCPServerEntry {
	for i := range entries {
		if mcpServerPrefix(entries[i].Source, entries[i].PluginName, entries[i].Name) == serverPrefix {
			return &entries[i]
		}
	}
	return nil
}

// isGramHostedMCPURL reports whether rawURL points at a Gram-managed MCP
// server. Exact host match, case-insensitive.
func isGramHostedMCPURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), gramHostedMCPHost)
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
