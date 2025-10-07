package middleware

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
)

var mcpOpenAccessControlRoutes = []string{
	"/.well-known/oauth-authorization-server/mcp",
	"/.well-known/oauth-protected-resource/mcp",
}

func CORSMiddleware(env string, serverURL string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch env {
			case "local":
				origin := r.Header.Get("Origin")
				if _, err := url.Parse(origin); err == nil {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
			case "dev":
				origin := r.Header.Get("Origin")
				// support preview urls
				if _, err := url.Parse(origin); err == nil && strings.Contains(origin, "speakeasyapi.vercel.app") {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else {
					w.Header().Set("Access-Control-Allow-Origin", serverURL)
				}
			case "prod":
				w.Header().Set("Access-Control-Allow-Origin", serverURL)
			default:
				// No CORS headers set for unspecified environments
			}

			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization, Gram-Session, Gram-Project, Gram-Token, idempotency-key, Gram-Admin-Override, Gram-Chat-ID")
			w.Header().Set("Access-Control-Expose-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, x-trace-id, Gram-Session, Gram-Chat-ID")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			// Special CORS policy for OAuth well-known endpoints
			// These need to be accessible from the browser on any origin
			if slices.ContainsFunc(mcpOpenAccessControlRoutes, func(route string) bool {
				return strings.HasPrefix(r.URL.Path, route)
			}) {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Access-Control-Allow-Methods", "GET")
			}

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
