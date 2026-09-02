package mcpservers

import (
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// AuthorizationMode answers one question about an mcp_server: which authority
// authenticates the inbound MCP client.
//
// It is deliberately not a restatement of mcp_servers.visibility. Visibility
// answers that question for three of its values but also carries an access
// control meaning (private enforces mcp:connect, public does not) that survives
// independently, and user_session_issuer_id answers it for a server of any
// visibility. Reading the two columns separately at each consumer is what let
// the two axes blur together; deriving the mode once keeps "who authenticates
// the caller" separate from "what may they reach".
//
// The mode is derived from the mcp_servers row alone. It is deliberately not
// derived for toolset-backed serving from toolsets.mcp_is_public or
// toolsets.external_oauth_server_id: those columns are the pre-mcp_servers way
// of saying the same thing, and teaching this type to speak both would
// entrench exactly the conflation AIM-25 exists to delete.
type AuthorizationMode string

const (
	// AuthorizationModeDisabled serves nothing.
	AuthorizationModeDisabled AuthorizationMode = "disabled"

	// AuthorizationModeGram makes Gram the authority: the caller presents a
	// Gram API key, dashboard session, or chat session. Private servers
	// require it; public servers probe for it and continue without.
	AuthorizationModeGram AuthorizationMode = "gram"

	// AuthorizationModeIssuerGated makes a Gram-hosted authorization server
	// bound to user_session_issuer_id the authority. The caller presents a
	// user-session JWT that Gram minted and validates.
	AuthorizationModeIssuerGated AuthorizationMode = "issuer-gated"

	// AuthorizationModeUpstream makes the server's own upstream authorization
	// server the authority. Gram advertises that server in its well-known
	// documents and forwards the inbound bearer unchanged, validating nothing
	// and minting no session.
	AuthorizationModeUpstream AuthorizationMode = "upstream"

	// AuthorizationModeInvalid is a row whose columns do not describe a
	// servable server. Callers must refuse to serve it. It is a state the
	// management API prevents rather than one the runtime trusts it to.
	AuthorizationModeInvalid AuthorizationMode = "invalid"
)

// ResolveAuthorizationMode derives the mode for an mcp_server row.
//
// The three states an `upstream` row may not hold are refused here rather than
// at each consumer, so a caller that forgets one cannot serve a row Gram has no
// coherent story for:
//
//   - No remote_session_issuer_id names no authorization server, so there is no
//     metadata to advertise and nothing for a client to authenticate against.
//   - A user_session_issuer_id would make ResyncMCPServerRemoteSessionIssuers
//     match the row on its next remote-session client attach or detach and
//     recompute the very issuer whose document is being served, leaving the
//     server advertising one authorization server while forwarding bearers to
//     another.
//   - Only the hosted (toolset) serve path forwards the inbound bearer as a
//     token input. Proxied backends resolve their upstream credential from a
//     remote session instead, so upstream there would silently serve
//     unauthenticated.
//
// The second return value explains an Invalid result and is for logs only. It
// must not reach a caller: an unauthenticated client learns nothing useful from
// a misconfigured server, and the operator's signal is the log line.
func ResolveAuthorizationMode(server *repo.McpServer) (AuthorizationMode, string) {
	if server == nil {
		return AuthorizationModeInvalid, "no mcp server row"
	}

	switch server.Visibility {
	case VisibilityDisabled:
		return AuthorizationModeDisabled, ""

	case VisibilityUpstream:
		switch {
		case !server.RemoteSessionIssuerID.Valid:
			return AuthorizationModeInvalid, "upstream authorization needs a remote_session_issuer_id naming the authorization server to advertise"
		case server.UserSessionIssuerID.Valid:
			return AuthorizationModeInvalid, "upstream authorization requires user_session_issuer_id to be NULL, or the remote session issuer resync would reassign the issuer being advertised"
		case !server.ToolsetID.Valid:
			return AuthorizationModeInvalid, "upstream authorization is served for hosted (toolset) backends only"
		default:
			return AuthorizationModeUpstream, ""
		}

	case VisibilityPrivate, VisibilityPublic:
		// Public tunneled servers carry an issuer because the schema forces one
		// on every tunneled backend, but they serve anonymously once the tunnel
		// owner has opted in, so the gate does not run for them. Consent itself
		// is checked at dispatch, not here.
		tunneledPublic := server.TunneledMcpServerID.Valid && server.Visibility == VisibilityPublic
		if server.UserSessionIssuerID.Valid && !tunneledPublic {
			return AuthorizationModeIssuerGated, ""
		}
		return AuthorizationModeGram, ""

	default:
		return AuthorizationModeInvalid, "unrecognized mcp server visibility"
	}
}
