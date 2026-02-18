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
	"/rpc/chatSessions.",
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

			// Validate the origin against the audience claim.
			// Browsers don't send Origin headers for same-origin GET/HEAD requests,
			// so if Origin is empty, verify the Host matches an allowed audience domain.
			origin := r.Header.Get("Origin")
			if origin != "" {
				if slices.Contains(claims.Audience, origin) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else {
					http.Error(w, fmt.Sprintf("Origin %s does not match audience claim: %s", origin, strings.Join(claims.Audience, ", ")), http.StatusForbidden)
					return
				}
			} else {
				// No Origin header - likely a same-origin request. Verify the Host
				// matches one of the audience domains to prevent bypass via stripped Origin.
				host := r.Host
				hostAllowed := false
				for _, aud := range claims.Audience {
					// Audience is a full URL like "https://app.getgram.ai", extract host
					audHost := strings.TrimPrefix(strings.TrimPrefix(aud, "https://"), "http://")
					if host == audHost {
						hostAllowed = true
						break
					}
				}
				if !hostAllowed {
					http.Error(w, fmt.Sprintf("Host %s does not match audience claim: %s", host, strings.Join(claims.Audience, ", ")), http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
