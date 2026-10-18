// Package authserver derives where a user session issuer's shared OAuth
// authorization server is served. It depends on nothing but the issuer row, so
// both the MCP service that serves the authorization servers and the
// management APIs that show their endpoints can use it.
package authserver

import (
	"errors"
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

var (
	// ErrNotShared reports an issuer that is not in shared mode, so it has no
	// shared authorization server.
	ErrNotShared = errors.New("user session issuer is not in shared mode")

	// ErrPinnedIssuerURLInvalid reports a shared-mode issuer whose pinned issuer
	// URL is not its own shared authorization server's.
	ErrPinnedIssuerURLInvalid = errors.New("pinned issuer URL is not the issuer's shared authorization server")

	// ErrHostNotServed reports a shared-mode issuer whose authorization server
	// would be on a host the deployment does not mount the shared routes on.
	ErrHostNotServed = errors.New("shared authorization server host is not served by this deployment")
)

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
// issuer's own (ErrPinnedIssuerURLInvalid), and one on a host the shared routes
// are not mounted on (ErrHostNotServed). An issuer not in shared mode returns
// ErrNotShared.
func (h Hosts) SharedIssuerURL(issuer repo.UserSessionIssuer) (string, error) {
	if !IssuerInSharedMode(issuer) {
		return "", fmt.Errorf("user session issuer %s: %w", issuer.ID, ErrNotShared)
	}
	path := SharedPath(issuer.ID)
	origin := ""
	if IssuerURLPinned(issuer) {
		pinned, err := url.Parse(issuer.PinnedIssuerUrl.String)
		if err != nil {
			return "", fmt.Errorf("invalid pinned issuer URL: %w: %w", ErrPinnedIssuerURLInvalid, err)
		}
		if pinned.RawQuery != "" || pinned.Fragment != "" || pinned.Path != path {
			return "", fmt.Errorf("pinned issuer URL %q: %w", issuer.PinnedIssuerUrl.String, ErrPinnedIssuerURLInvalid)
		}
		origin = requestorigin.URLOrigin(issuer.PinnedIssuerUrl.String)
		if origin == "" {
			// Without a scheme and host the pin names no server, rather than
			// one the deployment does not serve.
			return "", fmt.Errorf("pinned issuer URL %q has no origin: %w", issuer.PinnedIssuerUrl.String, ErrPinnedIssuerURLInvalid)
		}
	} else {
		base := h.ServerURL
		if issuer.UseAuthenticationHost && h.AuthenticationHostBaseURL != "" {
			base = h.AuthenticationHostBaseURL
		}
		origin = requestorigin.URLOrigin(base)
	}
	if !h.ServesSharedOn(origin) {
		return "", fmt.Errorf("shared authorization server issuer origin %q: %w", origin, ErrHostNotServed)
	}
	return origin + path, nil
}

// IssuerURLPinned reports whether a shared-mode issuer's authorization server
// issuer URL is pinned, so it stays the same when the deployment's hosts
// change. An unpinned issuer's URL is derived from the hosts on every read. A
// pin left on an issuer not in shared mode pins nothing.
func IssuerURLPinned(issuer repo.UserSessionIssuer) bool {
	return IssuerInSharedMode(issuer) && issuer.PinnedIssuerUrl.Valid && issuer.PinnedIssuerUrl.String != ""
}

// SharedAuthorizationServer is where an issuer's shared authorization server
// is served: the values an operator configures another platform with.
type SharedAuthorizationServer struct {
	// IssuerURL is the authorization server's RFC 8414 issuer identifier.
	IssuerURL string

	// TokenEndpoint is the authorization server's token endpoint.
	TokenEndpoint string
}

// SharedAuthorizationServer derives an issuer's shared authorization server.
// It fails as SharedIssuerURL does, for the same issuers.
func (h Hosts) SharedAuthorizationServer(issuer repo.UserSessionIssuer) (SharedAuthorizationServer, error) {
	issuerURL, err := h.SharedIssuerURL(issuer)
	if err != nil {
		return SharedAuthorizationServer{}, err
	}
	tokenEndpoint, err := SharedTokenEndpoint(issuerURL)
	if err != nil {
		return SharedAuthorizationServer{}, err
	}
	return SharedAuthorizationServer{IssuerURL: issuerURL, TokenEndpoint: tokenEndpoint}, nil
}

// AuthenticationHostAvailable reports whether the deployment has an
// authentication host to announce authorization servers on.
func (h Hosts) AuthenticationHostAvailable() bool {
	return h.AuthenticationHostBaseURL != ""
}

// SharedIssuerURLOn is the issuer identifier an issuer's shared authorization
// server takes on the authentication host, or otherwise on the server URL: the
// URL to pin when moving a pinned issuer between the two.
func (h Hosts) SharedIssuerURLOn(issuerID uuid.UUID, onAuthenticationHost bool) string {
	base := h.ServerURL
	if onAuthenticationHost && h.AuthenticationHostBaseURL != "" {
		base = h.AuthenticationHostBaseURL
	}
	return requestorigin.URLOrigin(base) + SharedPath(issuerID)
}

// PerServerOrigin is where an issuer in endpoint mode announces its MCP
// servers' own authorization servers: the authentication host when the issuer
// opts in to one the deployment has, and otherwise the server URL. MCP servers
// on a custom domain keep their domain either way.
func (h Hosts) PerServerOrigin(useAuthenticationHost bool) string {
	if useAuthenticationHost && h.AuthenticationHostBaseURL != "" {
		return requestorigin.URLOrigin(h.AuthenticationHostBaseURL)
	}
	return requestorigin.URLOrigin(h.ServerURL)
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
