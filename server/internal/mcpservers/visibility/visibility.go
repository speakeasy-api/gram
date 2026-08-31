// Package visibility declares the mcp_servers.visibility enum values.
// It mirrors the enum in the design package (design/mcpservers/design.go)
// and is a leaf package so that packages mcpservers itself depends on
// (e.g. plugins) can reference the values without an import cycle. Most
// callers should keep using the mcpservers.Visibility* aliases.
package visibility

const (
	Public   = "public"
	Private  = "private"
	Disabled = "disabled"

	// Upstream serves the MCP server's own upstream authorization server as
	// the authority for inbound clients: the well-known documents advertise
	// that server, taken from the remote_session_issuers.metadata snapshot
	// named by mcp_servers.remote_session_issuer_id, and the inbound bearer
	// is forwarded upstream unchanged. Gram validates nothing, mints no
	// session, and runs no consent, so it is not an authorization server for
	// these clients at all.
	//
	// Deliberately absent from the design enum until every site that would
	// mishandle it is fixed, so no operator can write the value early. The
	// serve path is hosted-backend only; every other backend fails closed.
	Upstream = "upstream"
)
