package relay

import (
	"context"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

const claudeMCPInventoryTimeout = 15 * time.Second

type mcpInventoryEntry struct {
	Name    string
	URL     string
	Command string
}

// collectClaudeMCPInventory asks Claude for the effective server list so
// plugin and claude.ai connector servers, which are absent from config files,
// are included. Collection is best-effort: hooks must continue when the CLI
// is unavailable, slow, or returns an unfamiliar format.
func collectClaudeMCPInventory(ctx context.Context, cwd string) []mcpInventoryEntry {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, claudeMCPInventoryTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "mcp", "list")
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	return parseClaudeMCPInventory(string(out))
}

// parseClaudeMCPInventory parses `<name>: <target> (<transport>) - <status>`.
// Names may contain colons, so delimiters are consumed from the right.
func parseClaudeMCPInventory(out string) []mcpInventoryEntry {
	var entries []mcpInventoryEntry
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		statusAt := strings.LastIndex(line, " - ")
		if line == "" || statusAt < 0 {
			continue
		}

		head := strings.TrimSpace(line[:statusAt])
		if strings.HasSuffix(head, ")") {
			if open := strings.LastIndex(head, " ("); open > 0 && upperAlpha(head[open+2:len(head)-1]) {
				head = strings.TrimSpace(head[:open])
			}
		}
		separator := strings.LastIndex(head, ": ")
		if separator < 0 {
			continue
		}
		name := strings.TrimSpace(head[:separator])
		target := strings.TrimSpace(head[separator+2:])
		if name == "" || target == "" {
			continue
		}
		if after, ok := strings.CutPrefix(name, "claude.ai "); ok {
			name = after
		} else if after, ok := strings.CutPrefix(name, "plugin:"); ok {
			if _, display, found := strings.Cut(after, ":"); found {
				name = display
			} else {
				name = after
			}
		}

		entry := mcpInventoryEntry{Name: name, URL: "", Command: ""}
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			entry.URL = target
		} else {
			entry.Command = target
		}
		entries = append(entries, entry)
	}
	return entries
}

func upperAlpha(s string) bool {
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

func attachMCPInventory(payload *components.IngestRequestBody, entries []mcpInventoryEntry) {
	if len(entries) == 0 {
		return
	}
	if payload.Data == nil {
		payload.Data = &components.HookIngestData{
			Mcp:               nil,
			McpAttribution:    nil,
			McpInventory:      nil,
			Message:           nil,
			Notification:      nil,
			Prompt:            nil,
			PromptAttachments: nil,
			Skill:             nil,
			ToolCall:          nil,
			Usage:             nil,
		}
	}
	payload.Data.McpInventory = make([]components.HookMCPData, 0, len(entries))
	for _, entry := range entries {
		redactedURL := ""
		if entry.URL != "" {
			var ok bool
			redactedURL, ok = redactMCPInventoryURL(entry.URL)
			if !ok {
				continue
			}
		}
		payload.Data.McpInventory = append(payload.Data.McpInventory, components.HookMCPData{
			ServerName:     optStr(entry.Name),
			ServerIdentity: optStr(entry.Name),
			URL:            optStr(redactedURL),
			Command:        optStr(redactCommand(entry.Command)),
			ResultJSON:     nil,
		})
	}
}

// redactMCPInventoryURL omits malformed absolute HTTP URLs from the snapshot.
// The hook still continues; only the unsafe inventory entry is skipped. The
// generic tool-call redactor preserves unparseable strings for observability,
// but a bulk-collected snapshot must not transmit a raw URL whose credentials
// could not be inspected.
func redactMCPInventoryURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	return redactURL(raw), true
}
