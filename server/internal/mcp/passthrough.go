package mcp

// isMCPPassthrough checks if a tool or resource has the mcp-passthrough meta tag.
// When true, the response should be returned as-is without additional formatting.
func isMCPPassthrough(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	if kind, ok := meta[MetaGramKind].(string); ok && kind == "mcp-passthrough" {
		return true
	}

	return false
}
