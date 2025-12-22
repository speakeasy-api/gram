package middleware

import (
	"net/http"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

var chatSessionsAllowedRoutes = []string{
	"/chat/completions",
	"/mcp",
	"/rpc/instances",
	"/rpc/chat",
}

// This isn't practical to do as a proper middleware because it needs to interoperate with the CORSMiddleware which does things like returning early for OPTIONS requests.
// Instead, we combine it with the CORSMiddleware so that all CORS stuff is handled in one place.
func chatSessionsCORS(chatSessionsManager *chatsessions.Manager) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "OPTIONS" {
				w.Header().Set("Access-Control-Allow-Origin", "*") // Allow all origins for OPTIONS requests because we don't know what origins to allow until we get the token on the actual request
				w.WriteHeader(http.StatusNoContent)
				return
			}

			chatSession := r.Header.Get(constants.ChatSessionsTokenHeader)
			if chatSession == "" {
				next.ServeHTTP(w, r)
				return
			}

			claims, err := chatSessionsManager.ValidateToken(r.Context(), chatSession)
			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			// If the request origin is in the allowed origins, set the allowed origin in the context to be used in the CORS middleware
			if slices.Contains(claims.Audience, r.Header.Get("Origin")) {
				w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			} else {
				w.WriteHeader(http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
