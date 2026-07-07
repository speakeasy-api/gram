package mcpservers

import (
	repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// EligibleForImplicitIssuer reports whether an mcp_server is implicitly
// gated by its project's default Gram issuer: private visibility, a remote
// or tunneled backend, and no explicit user_session_issuer_id (which
// always wins). Toolset-backed servers keep the legacy OAuth machinery.
func EligibleForImplicitIssuer(server *repo.McpServer) bool {
	return !server.UserSessionIssuerID.Valid &&
		server.Visibility == VisibilityPrivate &&
		(server.RemoteMcpServerID.Valid || server.TunneledMcpServerID.Valid)
}
