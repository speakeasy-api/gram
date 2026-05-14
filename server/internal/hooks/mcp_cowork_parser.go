package hooks

// ParseCoworkMCPInventory converts the structured MCP inventory shipped by
// the cowork branch of the SessionStart hook script into the same
// MCPServerEntry shape produced by ParseClaudeMCPList, so downstream
// consumers don't need to care which environment the snapshot came from.
//
// The inventory is extracted from cmux's per-run config file
// (local_<rid>.json -> remoteMcpServersConfig), which gives us the only
// host-side spot where the connector UUID is paired with the MCP server
// URL. Every entry is a remote HTTP server, so Transport is always "HTTP"
// and Status is "unknown" (no health check is performed on the host).
func ParseCoworkMCPInventory(raw any) []MCPServerEntry {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}

	entries := make([]MCPServerEntry, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		url, _ := m["url"].(string)
		uuid, _ := m["connector_uuid"].(string)
		source, _ := m["source"].(string)
		if source == "" {
			source = "claude.ai"
		}

		entries = append(entries, MCPServerEntry{
			Source:        source,
			Name:          name,
			URL:           url,
			Transport:     "HTTP",
			Status:        "unknown",
			ConnectorUUID: uuid,
		})
	}
	return entries
}
