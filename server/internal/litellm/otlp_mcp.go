package litellm

import (
	"encoding/json"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// mcpToolCallMetadataAttributeKeys are the span attribute keys under which
// LiteLLM's MCP gateway exports its StandardLoggingMCPToolCall payload.
// LiteLLM flattens request metadata onto the span as `metadata.<field>` and
// stringifies dict values, so the payload arrives as a JSON string. Some
// exporters prefix the flattened metadata with `litellm.`, the same way the
// allowlist accepts both spellings of the user_api_key_* keys.
var mcpToolCallMetadataAttributeKeys = []string{
	"metadata.mcp_tool_call_metadata",
	"litellm.metadata.mcp_tool_call_metadata",
}

// mcpToolCallMetadata is the subset of LiteLLM's StandardLoggingMCPToolCall
// that identifies the upstream server. The payload also carries the tool
// arguments and result, which are deliberately not decoded: they are request
// content and are kept out of telemetry the same way prompts are.
type mcpToolCallMetadata struct {
	// Name is the tool name as exposed by the upstream MCP server.
	Name string `json:"name"`

	// NamespacedToolName is the tool name as the agent saw it, prefixed with
	// the server alias, e.g. "github/create_issue".
	NamespacedToolName string `json:"namespaced_tool_name"`

	// MCPServerName is the alias the proxy operator gave the upstream server
	// in the LiteLLM config.
	MCPServerName string `json:"mcp_server_name"`

	// MCPServerResource is the upstream server origin (scheme, host and port)
	// the gateway forwarded the call to. Empty for stdio servers.
	MCPServerResource string `json:"mcp_server_resource"`
}

// parseMCPToolCallMetadata decodes the JSON payload of an MCP tool-call
// metadata attribute. It reports false when the payload is empty, oversized,
// not a JSON object, or identifies no server, so callers treat the span as a
// plain LiteLLM span.
func parseMCPToolCallMetadata(raw string) (mcpToolCallMetadata, bool) {
	var zero mcpToolCallMetadata
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxOTLPAttributeBytes || !strings.HasPrefix(raw, "{") {
		return zero, false
	}
	var md mcpToolCallMetadata
	if err := json.Unmarshal([]byte(raw), &md); err != nil {
		return zero, false
	}
	md.Name = strings.TrimSpace(md.Name)
	md.NamespacedToolName = strings.TrimSpace(md.NamespacedToolName)
	md.MCPServerName = strings.TrimSpace(md.MCPServerName)
	md.MCPServerResource = strings.TrimSpace(md.MCPServerResource)
	if md.MCPServerName == "" && md.MCPServerResource == "" {
		return zero, false
	}
	return md, true
}

// mcpSpanAttributes returns the gram.mcp.* and gram.tool_call.source
// attributes to add for an MCP gateway span, or nil when no server identity
// can be derived. The server URL is the upstream origin when the gateway
// reported one, so the row joins the same inventory entry as agents that call
// the server directly. A server known only by its alias gets the synthetic
// mcp-tool:// identity so the inventory can still key on it.
func mcpSpanAttributes(md mcpToolCallMetadata) map[attribute.Key]string {
	serverURL, host := mcpServerOrigin(md.MCPServerResource)
	if serverURL == "" {
		serverURL = shadowmcp.ToolNamespaceURL(md.MCPServerName)
		host = shadowmcp.ToolNamespaceServer(serverURL)
	}
	if serverURL == "" {
		return nil
	}
	source := strings.TrimSpace(md.MCPServerName)
	if source == "" {
		source = host
	}
	return map[attribute.Key]string{
		attr.MCPServerURLKey:   serverURL,
		attr.MCPMatchKey:       serverURL,
		attr.ToolCallSourceKey: source,
	}
}

// mcpServerOrigin returns the trimmed upstream origin and its host when the
// value parses as an absolute URL with a host, and empty strings otherwise.
func mcpServerOrigin(resource string) (string, string) {
	trimmed := strings.TrimSpace(resource)
	if trimmed == "" {
		return "", ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", ""
	}
	return trimmed, parsed.Host
}

// otlpMCPSpanAttributes reads the MCP tool-call metadata a LiteLLM gateway
// span carries and returns the derived gram.mcp.* attributes, or nil for spans
// that are not MCP tool calls. It reads the raw span attributes rather than
// the allowlisted ones so the payload itself is never retained on the row.
func otlpMCPSpanAttributes(values []otlpKeyValue) map[attribute.Key]string {
	for _, key := range mcpToolCallMetadataAttributeKeys {
		if md, ok := parseMCPToolCallMetadata(otlpStringAttribute(values, key)); ok {
			return mcpSpanAttributes(md)
		}
	}
	return nil
}
