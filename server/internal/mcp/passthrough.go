package mcp

// isMCPPassthrough checks if a tool or resource has the mcp-passthrough meta tag.
// When true, the response should be returned as-is without additional formatting.
func isMCPPassthrough(meta map[string]string) bool {
	return meta != nil && meta["gram.ai/kind"] == "mcp-passthrough"
}
