package relay

import (
	"net/url"
	"strings"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

func attachMCPInventory(data *components.HookIngestData, entries []agenthooks.MCPServer, complete bool) {
	data.McpInventory = make([]components.HookMCPData, 0, len(entries))
	data.McpInventoryCollected = &complete
	for _, entry := range entries {
		redactedURL := ""
		if entry.URL != "" {
			var ok bool
			redactedURL, ok = redactMCPInventoryURL(entry.URL)
			if !ok {
				continue
			}
		}
		data.McpInventory = append(data.McpInventory, components.HookMCPData{
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
