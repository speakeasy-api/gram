package hooks

import (
	"strings"
)

// MCPServerEntry is one parsed row from `claude mcp list`. We keep the raw
// line around for debugging because the CLI's output is unstable — if the
// format drifts we still log enough context to recover the original text.
type MCPServerEntry struct {
	RawLine       string `json:"raw_line"`
	Source        string `json:"source"`                // "claude.ai", "plugin", "local"
	PluginName    string `json:"plugin_name,omitempty"` // populated when Source == "plugin"
	Name          string `json:"name"`                  // server name as displayed
	URL           string `json:"url,omitempty"`         // populated for HTTP/SSE servers
	Command       string `json:"command,omitempty"`     // populated for stdio servers
	Transport     string `json:"transport,omitempty"`   // "HTTP", "SSE", "STDIO"
	Status        string `json:"status"`                // "connected", "needs_auth", "failed", "unknown"
	StatusRaw     string `json:"status_raw"`
	ConnectorUUID string `json:"connector_uuid,omitempty"` // populated for entries shipped by the cowork branch
}

// ParseClaudeMCPList parses the textual output of `claude mcp list` into
// structured entries. Lines that don't match the expected shape are skipped
// (e.g. the leading "Checking MCP server health…" preamble, blank lines).
func ParseClaudeMCPList(out string) []MCPServerEntry {
	var entries []MCPServerEntry
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry, ok := parseClaudeMCPListLine(line)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// parseClaudeMCPListLine handles one line. Expected shape:
//
//	<name>: <target>[ (<TRANSPORT>)] - <status>
//
// where <name> may contain colons (e.g. "plugin:slack:slack"), so we split
// from the right: status first, then optional transport, then the last
// ": " separates name from target.
func parseClaudeMCPListLine(line string) (MCPServerEntry, bool) {
	sepIdx := strings.LastIndex(line, " - ")
	if sepIdx < 0 {
		return MCPServerEntry{RawLine: "", Source: "", PluginName: "", Name: "", URL: "", Command: "", Transport: "", Status: "", StatusRaw: "", ConnectorUUID: ""}, false
	}
	head := strings.TrimSpace(line[:sepIdx])
	statusRaw := strings.TrimSpace(line[sepIdx+3:])

	transport := ""
	if strings.HasSuffix(head, ")") {
		if open := strings.LastIndex(head, " ("); open > 0 {
			inner := head[open+2 : len(head)-1]
			if isUpperAlpha(inner) {
				transport = inner
				head = strings.TrimSpace(head[:open])
			}
		}
	}

	colonIdx := strings.LastIndex(head, ": ")
	if colonIdx < 0 {
		return MCPServerEntry{RawLine: "", Source: "", PluginName: "", Name: "", URL: "", Command: "", Transport: "", Status: "", StatusRaw: "", ConnectorUUID: ""}, false
	}
	name := strings.TrimSpace(head[:colonIdx])
	target := strings.TrimSpace(head[colonIdx+2:])
	if name == "" || target == "" {
		return MCPServerEntry{RawLine: "", Source: "", PluginName: "", Name: "", URL: "", Command: "", Transport: "", Status: "", StatusRaw: "", ConnectorUUID: ""}, false
	}

	e := MCPServerEntry{
		RawLine:       line,
		Source:        "",
		PluginName:    "",
		Name:          "",
		URL:           "",
		Command:       "",
		Transport:     transport,
		Status:        classifyMCPStatus(statusRaw),
		StatusRaw:     statusRaw,
		ConnectorUUID: "",
	}
	e.Source, e.PluginName, e.Name = classifyMCPName(name)

	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		e.URL = target
		if e.Transport == "" {
			e.Transport = "HTTP"
		}
	} else {
		e.Command = target
		if e.Transport == "" {
			e.Transport = "STDIO"
		}
	}
	return e, true
}

func classifyMCPName(raw string) (source, plugin, name string) {
	if after, ok := strings.CutPrefix(raw, "claude.ai "); ok {
		return "claude.ai", "", after
	}
	if after, ok := strings.CutPrefix(raw, "plugin:"); ok {
		rest := after
		if i := strings.Index(rest, ":"); i > 0 {
			return "plugin", rest[:i], rest[i+1:]
		}
		return "plugin", "", rest
	}
	return "local", "", raw
}

func classifyMCPStatus(raw string) string {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "failed"):
		return "failed"
	case strings.Contains(lower, "needs authentication"):
		return "needs_auth"
	case strings.Contains(lower, "connected"):
		return "connected"
	default:
		return "unknown"
	}
}

func isUpperAlpha(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
