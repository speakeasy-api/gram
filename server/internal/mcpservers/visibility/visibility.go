// Package visibility declares the mcp_servers.visibility enum values.
//
// The column answers one question: who authenticates an inbound MCP client,
// and therefore whether and how the server is served.
//
//   - Disabled: nothing is served.
//   - Private: Gram, against a Gram API key, dashboard session, or user
//     session issuer, with mcp:connect enforced.
//   - Public: nobody, so the server is served anonymously.
//   - Upstream: the server's own upstream authorization server, whose metadata
//     Gram advertises and to which the inbound bearer is forwarded unchanged.
//
// "The visibility of an MCP server" is what this used to say, which is why the
// axis kept getting confused with access control and with which authorization
// server issued an upstream's credential. Neither is what it means. Public is
// not renamed to match, because a rename would cost a data migration, a
// breaking SDK enum change, and dashboard updates for clarity this comment
// already carries.
//
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
	// The serve path is hosted-backend only; every other backend fails closed,
	// in the management API at write time and in ResolveAuthorizationMode at
	// request time.
	Upstream = "upstream"
)
