package middleware

import (
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
)

// sharedOAuthCORSMethod returns the browser-readable method and whether path
// belongs to the shared authorization server namespace. An empty method keeps
// authorize, consent, and unknown routes out of credentialed platform CORS.
// Classification is independent of the request method, including preflights.
func sharedOAuthCORSMethod(path string) (method string, shared bool) {
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
