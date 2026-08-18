package middleware

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
)

var mcpOpenAccessControlRoutes = []string{
	"/.well-known/oauth-authorization-server/mcp",
	"/.well-known/oauth-protected-resource/mcp",
	"/.well-known/oauth-authorization-server/platform-mcp",
	"/.well-known/oauth-protected-resource/platform-mcp",
	"/openapi.yaml",
}

func CORSMiddleware(env string, serverURL string, chatSessionsManager *chatsessions.Manager) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch env {
			case "local":
				origin := r.Header.Get("Origin")
				if _, err := url.Parse(origin); err == nil {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
			case "dev":
				w.Header().Set("Access-Control-Allow-Origin", serverURL)
			case "prod":
				w.Header().Set("Access-Control-Allow-Origin", serverURL)
			default:
				// No CORS headers set for unspecified environments
			}

			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
			// The 2026-07-28 Mcp-Param-{Name} family (mirrored tool
			// arguments) is deliberately absent: the family is open-ended,
			// Access-Control-Allow-Headers has no prefix wildcard, and "*"
			// is invalid alongside Allow-Credentials. Browser support for it
			// requires echoing Access-Control-Request-Headers instead of a
			// static list.
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization, User-Agent, Gram-Session, Gram-Project, Gram-Key, Gram-Token, idempotency-key, Gram-Admin-Override, Gram-Chat-ID, Gram-Assistant-ID, Gram-Chat-Session, MCP-Protocol-Version, Mcp-Method, Mcp-Name, Mcp-Session-Id, X-Gram-Scope-Override, X-Gram-Source")
			w.Header().Set("Access-Control-Expose-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, x-trace-id, Gram-Session, Gram-Chat-ID, Gram-Chat-Session, Mcp-Session-Id")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			// Special CORS policy for OAuth well-known endpoints
			// These need to be accessible from the browser on any origin
			if slices.ContainsFunc(mcpOpenAccessControlRoutes, func(route string) bool {
				return strings.HasPrefix(r.URL.Path, route)
			}) {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Access-Control-Allow-Methods", "GET")
				w.Header().Del("Access-Control-Allow-Credentials")
			}

			// Special CORS handling for chat sessions-enabled routes
			if slices.ContainsFunc(chatSessionsAllowedRoutes, func(route string) bool {
				return strings.HasPrefix(r.URL.Path, route)
			}) {
				chatSessionsCORS(chatSessionsManager)(next).ServeHTTP(w, r)
				return
			}

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
