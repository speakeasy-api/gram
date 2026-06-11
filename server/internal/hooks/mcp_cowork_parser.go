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
		url, _ := m["url"].(string)
		uuid, _ := m["connector_uuid"].(string)
		source, _ := m["source"].(string)
		// connector_uuid is the only field that lets us re-match this
		// entry against an incoming `mcp__<uuid>__tool` call, so an
		// entry with neither name nor uuid is useless and we drop it.
		// Empty Name is allowed — downstream callers fall back to the
		// uuid as the source name in that case.
		if name == "" && uuid == "" {
			continue
		}
		if source == "" {
			source = "claude.ai"
		}

		entries = append(entries, MCPServerEntry{
			RawLine:       "",
			Source:        source,
			PluginName:    "",
			Name:          name,
			URL:           url,
			Command:       "",
			Transport:     "HTTP",
			Status:        "unknown",
			StatusRaw:     "",
			ConnectorUUID: uuid,
			ToolPrefix:    "",
		})
	}
	return entries
}
