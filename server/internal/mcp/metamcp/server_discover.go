package metamcp

import "encoding/json"

// DiscoverResult is the response shape for MCP 2026-07-28's sessionless
// server/discover method: the same self-description initialize answers, minus
// any session establishment, plus the set of protocol revisions the surface
// can serve. ServerInfo carries the serving package's server-identity value
// opaquely so the wire shape stays colocated with the other views.
type DiscoverResult struct {
	ProtocolVersions []string                   `json:"protocolVersions"`
	Capabilities     map[string]json.RawMessage `json:"capabilities"`
	ServerInfo       any                        `json:"serverInfo"`
	Instructions     string                     `json:"instructions,omitempty"`
}
