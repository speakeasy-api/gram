package shadowmcp

import (
	"net/url"
	"strings"
)

// ToolNamespaceScheme is the URI scheme of a synthetic server identity for an
// MCP server known only by the <server> segment of a namespaced tool name
// (`mcp__<server>__<tool>`). LLM proxies such as LiteLLM see an agent's tool
// definitions and tool calls but never the MCP server URL behind them, so the
// server segment is the only identity they can report.
//
// The identity is shaped as a URL on purpose: every inventory layer — the
// telemetry attributes, trace summaries, `shadow_mcp_inventory_urls`, the
// access service and the dashboard — is keyed on a canonical URL string, and
// `CanonicalizeInventoryURL` only requires a scheme and a host. A distinct
// scheme keeps these rows recognisable as name-grade (see IsToolNamespaceURL)
// so they are never mistaken for a resolved http(s) server: the scanner treats
// them as unresolved and the inventory records review decisions on them
// without writing enforcement, mirroring stdio commands.
const ToolNamespaceScheme = "mcp-tool"

const toolNamespacePrefix = ToolNamespaceScheme + "://"

// ToolNamespaceURL returns the synthetic identity URI for a server known only
// by name, e.g. "github" -> "mcp-tool://github". The name is trimmed and
// lower-cased so the same server observed with different casing collapses to
// one inventory row. It returns "" when the name is empty or cannot be carried
// as a URL host (whitespace, slashes, or other characters url.Parse rejects),
// so callers can drop the entry the same way they drop entries with no URL.
func ToolNamespaceURL(serverName string) string {
	name := strings.ToLower(strings.TrimSpace(serverName))
	if name == "" || strings.ContainsAny(name, " \t\r\n/?#@:\\") {
		return ""
	}
	candidate := toolNamespacePrefix + name
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.Scheme != ToolNamespaceScheme || parsed.Host != name || parsed.Path != "" {
		return ""
	}
	return candidate
}

// IsToolNamespaceURL reports whether value is a synthetic tool-namespace
// identity produced by ToolNamespaceURL (or its canonicalized form).
func IsToolNamespaceURL(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), toolNamespacePrefix)
}

// ToolNamespaceServer returns the server name carried by a tool-namespace
// identity URI, or "" when value is not one.
func ToolNamespaceServer(value string) string {
	trimmed := strings.TrimSpace(value)
	if !IsToolNamespaceURL(trimmed) {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}
