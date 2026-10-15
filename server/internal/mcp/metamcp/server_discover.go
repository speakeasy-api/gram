package metamcp

import "encoding/json"

// DiscoverResult is the sessionless MCP 2026-07-28 server description.
// The serving package supplies identity and caching fields in its envelope.
type DiscoverResult struct {
	// SupportedVersions lists the revisions served by this surface.
	SupportedVersions []string `json:"supportedVersions"`

	// Capabilities describes the operations offered by this server.
	Capabilities map[string]json.RawMessage `json:"capabilities"`

	// Instructions provides operator guidance for using this server.
	Instructions string `json:"instructions,omitempty"`
}
