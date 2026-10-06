package usersessions

import (
	"github.com/google/uuid"
)

// AuthorizationServerMode is a user session issuer's stored
// authorization_server_mode: which OAuth authorization server its MCP
// servers point clients to.
type AuthorizationServerMode string

const (
	// AuthorizationServerModeEndpoint gives every MCP server attached to the
	// issuer its own authorization server, rooted at the server's URL. It is
	// the column default, so every issuer created before shared mode existed
	// has it.
	AuthorizationServerModeEndpoint AuthorizationServerMode = "endpoint"

	// AuthorizationServerModeShared serves one authorization server for the
	// issuer, at SharedAuthorizationServerPath, used by all of its MCP servers.
	AuthorizationServerModeShared AuthorizationServerMode = "shared"
)

// SharedAuthorizationServerPathPrefix is the path every shared authorization
// server is rooted under, ahead of the issuer id.
const SharedAuthorizationServerPathPrefix = "/oauth/usi/"

// SharedAuthorizationServerPath is the path of an issuer's shared
// authorization server: its RFC 8414 issuer identifier is this path on the
// host that serves it.
func SharedAuthorizationServerPath(issuerID uuid.UUID) string {
	return SharedAuthorizationServerPathPrefix + issuerID.String()
}
