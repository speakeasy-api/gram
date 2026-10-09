// Package authserver derives where a user session issuer's shared OAuth
// authorization server is served. It depends on nothing but the issuer row, so
// both the MCP service that serves the authorization servers and the
// management APIs that show their endpoints can use it.
package authserver

import (
	"fmt"
	"net/url"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// Mode is a user session issuer's stored
// authorization_server_mode: which OAuth authorization server its MCP
// servers point clients to.
type Mode string

const (
	// ModeEndpoint gives every MCP server attached to the
	// issuer its own authorization server, rooted at the server's URL. It is
	// the column default, so every issuer created before shared mode existed
	// has it.
	ModeEndpoint Mode = "endpoint"

	// ModeShared serves one authorization server for the
	// issuer, at SharedPath, used by all of its MCP servers.
	ModeShared Mode = "shared"
)

// SharedPathPrefix is the path every shared authorization
// server is rooted under, ahead of the issuer id.
const SharedPathPrefix = "/oauth/usi/"

// SharedPath is the path of an issuer's shared
// authorization server: its RFC 8414 issuer identifier is this path on the
// host that serves it.
func SharedPath(issuerID uuid.UUID) string {
	return SharedPathPrefix + issuerID.String()
}

// IssuerInSharedMode reports whether an issuer serves a shared authorization
// server.
func IssuerInSharedMode(issuer repo.UserSessionIssuer) bool {
	return Mode(issuer.AuthorizationServerMode) == ModeShared
}

// Hosts are the deployment's hosts that decide where
// a shared authorization server is served. The MCP service, which serves the
// authorization servers, and the management API, which shows their endpoints,
// both derive issuers from it so the two cannot disagree.
type Hosts struct {
	// ServerURL is the deployment's server URL, the default origin.
	ServerURL string

	// AuthenticationHostBaseURL is the authentication host's base URL, empty
	// when the deployment has none.
	AuthenticationHostBaseURL string

	// PlatformHosts maps the deployment's extra first-party hosts to their
	// base URLs. The shared routes are served on these as well.
	PlatformHosts map[string]string
}

// SharedIssuerURL is the RFC 8414 issuer identifier of an issuer's shared
// authorization server. The pinned issuer URL wins when set. Otherwise the
// issuer is derived: the authentication host when the issuer opts in to it and
// one is configured, else the server URL, followed by the issuer's path.
//
// An issuer naming a server this deployment does not serve is refused, so its
// MCP servers keep their per-endpoint authorization servers rather than
// pointing clients at a 404: a pinned issuer URL whose path is not the
// issuer's own, and one on a host the shared routes are not mounted on.
func (h Hosts) SharedIssuerURL(issuer repo.UserSessionIssuer) (string, error) {
	if !IssuerInSharedMode(issuer) {
		return "", fmt.Errorf("user session issuer %s is not in shared mode", issuer.ID)
	}
	path := SharedPath(issuer.ID)
	origin := ""
	if issuer.PinnedIssuerUrl.Valid && issuer.PinnedIssuerUrl.String != "" {
		pinned, err := url.Parse(issuer.PinnedIssuerUrl.String)
		if err != nil {
			return "", fmt.Errorf("parse pinned issuer URL: %w", err)
		}
		if pinned.RawQuery != "" || pinned.Fragment != "" || pinned.Path != path {
			return "", fmt.Errorf("pinned issuer URL %q is not this issuer's shared authorization server", issuer.PinnedIssuerUrl.String)
		}
		origin = requestorigin.URLOrigin(issuer.PinnedIssuerUrl.String)
	} else {
		base := h.ServerURL
		if issuer.UseAuthenticationHost && h.AuthenticationHostBaseURL != "" {
			base = h.AuthenticationHostBaseURL
		}
		origin = requestorigin.URLOrigin(base)
	}
	if !h.ServesSharedOn(origin) {
		return "", fmt.Errorf("shared authorization server issuer origin %q is not served by this deployment", origin)
	}
	return origin + path, nil
}

// SharedTokenEndpoint is the token endpoint of the shared authorization
// server whose issuer identifier is issuerURL.
func SharedTokenEndpoint(issuerURL string) (string, error) {
	token, err := url.JoinPath(issuerURL, "token")
	if err != nil {
		return "", fmt.Errorf("build shared token URL: %w", err)
	}
	return token, nil
}

// ServesSharedOn reports whether the shared authorization
// server routes are served on origin: the server URL's, the authentication
// host's, or an extra platform host's. Comparing whole origins also pins the
// scheme, so a pinned issuer is https wherever the deployment is.
func (h Hosts) ServesSharedOn(origin string) bool {
	if origin == "" {
		return false
	}
	if origin == requestorigin.URLOrigin(h.ServerURL) {
		return true
	}
	if h.AuthenticationHostBaseURL != "" && origin == requestorigin.URLOrigin(h.AuthenticationHostBaseURL) {
		return true
	}
	for _, baseURL := range h.PlatformHosts {
		if origin == requestorigin.URLOrigin(baseURL) {
			return true
		}
	}
	return false
}
