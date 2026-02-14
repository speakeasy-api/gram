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
}

func CORSMiddleware(env string, serverURL string, chatSessionsManager *chatsessions.Manager, extraOrigins ...string) func(next http.Handler) http.Handler {
	allowedOrigins := append([]string{serverURL}, extraOrigins...)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch env {
			case "local":
				origin := r.Header.Get("Origin")
				if _, err := url.Parse(origin); err == nil {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
			case "dev", "prod":
				origin := r.Header.Get("Origin")
				if slices.Contains(allowedOrigins, origin) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else {
					w.Header().Set("Access-Control-Allow-Origin", serverURL)
				}
			default:
				// No CORS headers set for unspecified environments
			}

			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization, User-Agent, Gram-Session, Gram-Project, Gram-Token, idempotency-key, Gram-Admin-Override, Gram-Chat-ID, Gram-Chat-Session, MCP-Protocol-Version")
			w.Header().Set("Access-Control-Expose-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, x-trace-id, Gram-Session, Gram-Chat-ID, Gram-Chat-Session")
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
