package middleware

import (
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
)

// sharedOAuthCORSMethod returns the browser-readable method and whether path
// belongs to a shared or Channels OAuth namespace. An empty method keeps
// authorize, consent, and unknown routes out of credentialed platform CORS.
// Classification is independent of the request method, including preflights.
func sharedOAuthCORSMethod(path string) (method string, shared bool) {
	if resource, ok := strings.CutPrefix(path, wellknown.OAuthProtectedResourcePath); ok && isChannelsEndpoint(resource, "mcp") {
		return http.MethodGet, true
	}
	if isChannelsEndpoint(path, "api") || isChannelsEndpoint(path, "mcp") {
		// The API retains platform CORS; MCP has its own bearer-only policy.
		return "", false
	}
	switch path {
	case "/.well-known/oauth-authorization-server/channels/oauth":
		return http.MethodGet, true
	case "/channels/oauth/register", "/channels/oauth/token", "/channels/oauth/revoke":
		return http.MethodPost, true
	}
	for _, namespace := range []string{
		"/channels",
		wellknown.OAuthAuthorizationServerPath + "/channels",
		wellknown.OAuthProtectedResourcePath + "/channels",
	} {
		if path == namespace || strings.HasPrefix(path, namespace+"/") {
			// Never let consent or malformed/sibling routes fall through to
			// credentialed platform CORS, including in local development.
			return "", true
		}
	}

	if issuerID, ok := strings.CutPrefix(path, wellknown.OAuthAuthorizationServerPath+authserver.SharedPathPrefix); ok {
		if issuerID != "" && !strings.Contains(issuerID, "/") {
			return http.MethodGet, true
		}
		return "", true
	}

	rest, ok := strings.CutPrefix(path, authserver.SharedPathPrefix)
	if !ok {
		return "", false
	}
	issuerID, endpoint, found := strings.Cut(rest, "/")
	if !found || issuerID == "" {
		return "", true
	}
	switch "/" + endpoint {
	case wellknown.OAuthAuthorizationServerPath:
		return http.MethodGet, true
	case "/token", "/register", "/revoke":
		return http.MethodPost, true
	default:
		return "", true
	}
}
