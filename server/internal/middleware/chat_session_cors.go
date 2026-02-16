package middleware

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

var chatSessionsAllowedRoutes = []string{
	"/chat/completions",
	"/mcp",
	"/rpc/chat.",
}

// This isn't practical to do as a proper middleware because it needs to interoperate with the CORSMiddleware which does things like returning early for OPTIONS requests.
// Instead, we combine it with the CORSMiddleware so that all CORS stuff is handled in one place.
func chatSessionsCORS(chatSessionsManager *chatsessions.Manager) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				// Slightly non-ideal, but later in the file we validate the origin of the request against the audience claim
				w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin")) // Allow the origin of the request for OPTIONS requests because we don't know what origins to allow until we get the token on the actual request
				// Echo back whatever headers the client requested - this allows arbitrary headers
				if requestedHeaders := r.Header.Get("Access-Control-Request-Headers"); requestedHeaders != "" {
					w.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			chatSession := r.Header.Get(constants.ChatSessionsTokenHeader)
			if chatSession == "" {
				// If the request uses API key auth (e.g. dangerousApiKey from Elements),
				// allow the requesting origin so the browser doesn't block the response.
				if r.Header.Get(constants.APIKeyHeader) != "" {
					w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
				}
				next.ServeHTTP(w, r)
				return
			}

			claims, err := chatSessionsManager.ValidateToken(r.Context(), chatSession)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// If the request origin is in the allowed origins, set the allowed origin in the context to be used in the CORS middleware
			if slices.Contains(claims.Audience, r.Header.Get("Origin")) {
				w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			} else {
				http.Error(w, fmt.Sprintf("Origin %s does not match audience claim: %s", r.Header.Get("Origin"), strings.Join(claims.Audience, ", ")), http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
