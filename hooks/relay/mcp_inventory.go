package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

const (
	claudeMCPInventoryTimeout = 15 * time.Second
	// Codex resolves its list from config layers rather than by probing
	// servers, so it returns far faster than Claude's.
	codexMCPInventoryTimeout = 5 * time.Second
)

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

// collectCodexMCPInventory asks Codex for its effective server list. Codex has
// no plugin/connector servers of its own, but the list still resolves the
// merged managed/user/project config layers that a hook event cannot see, and
// the shadow-MCP guard needs it to prove where a tool call actually routes.
// The project layer resolves relative to the working directory, so the list is
// taken from the session's cwd exactly as the Claude collector does.
// Best-effort for the same reason: a missing or slow CLI must not hold up the
// hook.
//
// Not replayed: per-session launch overrides (--profile, -c). agenthooks
// replays them for its own resolution because they change the effective server
// set. Without them a profile-only server is missing from the snapshot (its
// meta-tool reads then deny), and a `-c mcp_servers.x.url=…` override is
// invisible (the snapshot reports the default target for that name). See
// DNO-770.
func collectCodexMCPInventory(ctx context.Context, cwd string) []mcpInventoryEntry {
	binary := findCodexBinary()
	if binary == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, codexMCPInventoryTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "mcp", "list", "--json")
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	return parseCodexMCPInventory(out)
}

// parseCodexMCPInventory reads `codex mcp list --json`. The server parses the
// same document (hooks.ParseCodexMCPList) for the legacy Codex endpoint, so
// the two must agree on the shape: an array of servers, each with a transport
// object holding either a url or a command plus args.
func parseCodexMCPInventory(out []byte) []mcpInventoryEntry {
	var servers []struct {
		Name      string `json:"name"`
		Enabled   *bool  `json:"enabled"`
		Transport struct {
			URL     string   `json:"url"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"transport"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &servers) != nil {
		return nil
	}

	var entries []mcpInventoryEntry
	for _, server := range servers {
		// An explicitly disabled server cannot serve the call, so listing it
		// would let a disabled entry vouch for a tool that routed elsewhere.
		if server.Enabled != nil && !*server.Enabled {
			continue
		}
		name := strings.TrimSpace(server.Name)
		url := strings.TrimSpace(server.Transport.URL)
		command := strings.TrimSpace(server.Transport.Command)
		if command != "" {
			for _, arg := range server.Transport.Args {
				if arg = strings.TrimSpace(arg); arg != "" {
					command += " " + arg
				}
			}
		}
		if name == "" || (url == "" && command == "") {
			continue
		}
		entries = append(entries, mcpInventoryEntry{Name: name, URL: url, Command: command})
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
