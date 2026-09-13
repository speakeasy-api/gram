package litellm

import (
	"net/url"
	"sort"
	"strings"

	hooksgen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// mcpInventoryFromTools derives the MCP servers an agent has configured from
// the tool definitions LiteLLM forwarded with a model request. The proxy
// never sees an MCP server URL behind a namespaced function tool
// (`mcp__<server>__<tool>`), so those servers get the synthetic
// shadowmcp.ToolNamespaceURL identity; Responses-API hosted MCP tool entries
// (`type: "mcp"`) carry their server_url and keep it. Bare function tools,
// built-in tools, and the Cursor `MCP:<fn>` form (no server segment) are
// ignored. Entries are de-duplicated by URL and sorted by URL for stable
// output. Returns nil when nothing in tools names an MCP server.
func mcpInventoryFromTools(tools []any) []*hooksgen.HookMCPData {
	byURL := map[string]*hooksgen.HookMCPData{}
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(anyString(tool["type"]))) {
		case "function":
			fn, _ := tool["function"].(map[string]any)
			name := strings.TrimSpace(anyString(fn["name"]))
			if !strings.HasPrefix(name, "mcp__") || !toolref.IsMCPToolName(name) {
				continue
			}
			server := strings.ToLower(toolref.MCPServerOf(name))
			identity := shadowmcp.ToolNamespaceURL(server)
			if identity == "" {
				continue
			}
			byURL[identity] = &hooksgen.HookMCPData{
				ServerName:     new(server),
				ServerIdentity: nil,
				URL:            new(identity),
				Command:        nil,
				ResultJSON:     nil,
			}
		case "mcp":
			serverURL := strings.TrimSpace(anyString(tool["server_url"]))
			if serverURL == "" {
				continue
			}
			label := strings.TrimSpace(anyString(tool["server_label"]))
			if label == "" {
				if parsed, err := url.Parse(serverURL); err == nil {
					label = parsed.Host
				}
			}
			byURL[serverURL] = &hooksgen.HookMCPData{
				ServerName:     new(label),
				ServerIdentity: nil,
				URL:            new(serverURL),
				Command:        nil,
				ResultJSON:     nil,
			}
		}
	}
	if len(byURL) == 0 {
		return nil
	}
	out := make([]*hooksgen.HookMCPData, 0, len(byURL))
	for _, entry := range byURL {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return *out[i].URL < *out[j].URL })
	return out
}

// anyString returns v when it is a string and "" for every other JSON value,
// so a numeric or object-valued field reads as absent rather than as its
// fmt.Sprint rendering.
func anyString(v any) string {
	text, _ := v.(string)
	return text
}
