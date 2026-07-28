package hooks

import (
	"strings"
)

// ParseCodexMCPList converts the structured MCP inventory shipped by the
// Codex SessionStart hook script into the same MCPServerEntry shape produced
// by ParseClaudeMCPList, so downstream consumers don't need to care which
// assistant the snapshot came from.
//
// The inventory is the parsed output of `codex mcp list --json` (verified
// against codex-cli 0.139.0): an array of objects with `name`, `enabled`,
// `auth_status`, and a `transport` object whose `type` discriminates
// `streamable_http` (with `url`) from `stdio` (with `command` + `args`).
// Disabled servers are skipped — they cannot produce tool calls, and the
// cached snapshot represents the active configuration.
//
// Codex derives `mcp__<server>__<tool>` prefixes from the config name via
// its own sanitizer, which differs from Claude's — every character outside
// [A-Za-z0-9_] becomes "_" (so "int-linear" appears as "int_linear"). Each
// entry carries that pre-computed prefix in ToolPrefix for matching.
func ParseCodexMCPList(raw any) []MCPServerEntry {
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
		if enabled, ok := m["enabled"].(bool); ok && !enabled {
			continue
		}

		var transportType, url, command string
		if t, ok := m["transport"].(map[string]any); ok {
			transportType, _ = t["type"].(string)
			url, _ = t["url"].(string)
			command, _ = t["command"].(string)
			if args, ok := t["args"].([]any); ok && command != "" {
				for _, a := range args {
					if s, ok := a.(string); ok && s != "" {
						command += " " + s
					}
				}
			}
		}
		if url == "" && command == "" {
			continue
		}

		transport := "HTTP"
		switch {
		case command != "":
			transport = "STDIO"
		case strings.Contains(transportType, "sse"):
			transport = "SSE"
		}

		authStatus, _ := m["auth_status"].(string)

		entries = append(entries, MCPServerEntry{
			RawLine:       "",
			Source:        "local",
			PluginName:    "",
			Name:          name,
			URL:           url,
			Command:       command,
			Transport:     transport,
			Status:        "unknown",
			StatusRaw:     authStatus,
			ConnectorUUID: "",
			ToolPrefix:    codexSanitizeToolName(name),
		})
	}
	return entries
}

// codexSanitizeToolName mirrors codex-rs sanitize_responses_api_tool_name
// (codex-rs/codex-mcp/src/mcp/mod.rs): every character outside [A-Za-z0-9_]
// becomes "_", and empty input becomes "_". Codex additionally appends a
// hash suffix when two sanitized namespaces collide — such entries simply
// fail to match here, which downstream consumers treat as no inventory
// objection.
func codexSanitizeToolName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}
