package mcpservers

import (
	repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// EligibleForImplicitIssuer reports whether an mcp_server with no explicit
// user_session_issuer binding is implicitly gated by its project's default
// Gram issuer: private visibility and a remote or tunneled backend. Such
// servers accept Gram-minted user-session JWTs (with the project-default
// issuer materialised on demand) while still falling back to the legacy
// identity chain (API keys, chat sessions) for callers that have one.
//
// Toolset-backed servers are excluded — they keep the legacy OAuth
// machinery — and an explicit user_session_issuer_id always wins: callers
// must check the column before consulting this predicate.
func EligibleForImplicitIssuer(server *repo.McpServer) bool {
	return !server.UserSessionIssuerID.Valid &&
		server.Visibility == VisibilityPrivate &&
		(server.RemoteMcpServerID.Valid || server.TunneledMcpServerID.Valid)
}
