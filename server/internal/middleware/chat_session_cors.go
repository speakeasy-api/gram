package middleware

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

var chatSessionsAllowedRoutes = []string{
	"/chat/completions",
	"/chat/turnstream",
	"/mcp",
	"/rpc/chat.",
	"/rpc/chatSessions.",
}

// gramKeyCORSRoutes are the routes that actually authenticate with a Gram-Key
// header and therefore need the requesting origin echoed back so the browser
// will surface the response. Elements' dangerousApiKey flow bootstraps against
// /rpc/chatSessions.create before any chat session exists, and the chat routes
// accept the key directly (see chat.Service.Authorize).
//
// This is deliberately an allowlist rather than "every chatSessionsAllowedRoutes
// entry". The /mcp prefix in that list also covers /mcp/{slug} and the OAuth
// sub-routes (/token, /register, /authorize, /connect), none of which read
// Gram-Key at all — nothing under internal/mcp or internal/xmcp consults it,
// as MCP identity auth reads Authorization or Gram-Chat-Session only. Echoing
// the origin there authenticated nothing and let any page read a response it
// should not have been able to: a hostile origin that attached a dummy
// Gram-Key got Access-Control-Allow-Origin plus Allow-Credentials on a
// credential-free public MCP server, making tools/list and tools/call results
// readable cross-site.
var gramKeyCORSRoutes = []string{
	"/chat/completions",
	"/chat/turnstream",
	"/rpc/chat.",
	"/rpc/chatSessions.",
}

// ChatSessionValidator validates a chat-session token. Narrowed from
// *chatsessions.Manager so this package stays testable without standing up
// Redis: Manager.ValidateToken checks token revocation on the happy path,
// which would otherwise drag a testcontainer into every CORS test.
type ChatSessionValidator interface {
	ValidateToken(ctx context.Context, token string) (*chatsessions.ChatSessionClaims, bool, error)
}

// chatSessionOriginTrustedKey marks a request whose Origin was validated
// against a chat-session token's audience claim. The key type is unexported so
// only this package can set it — a forgeable marker would hand any caller the
// MCPSecurity exemption.
type chatSessionOriginTrustedKey struct{}

func markChatSessionOriginTrusted(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), chatSessionOriginTrustedKey{}, true))
}

func chatSessionOriginTrusted(ctx context.Context) bool {
	trusted, _ := ctx.Value(chatSessionOriginTrustedKey{}).(bool)
	return trusted
}

// This isn't practical to do as a proper middleware because it needs to interoperate with the CORSMiddleware which does things like returning early for OPTIONS requests.
// Instead, we combine it with the CORSMiddleware so that all CORS stuff is handled in one place.
func chatSessionsCORS(validator ChatSessionValidator) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				// Slightly non-ideal, but later in the file we validate the origin of the request against the audience claim
				w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin")) // Allow the origin of the request for OPTIONS requests because we don't know what origins to allow until we get the token on the actual request
				// Echo back whatever headers the client requested - this allows arbitrary headers
				if requestedHeaders := r.Header.Get("Access-Control-Request-Headers"); requestedHeaders != "" {
					w.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
				}
				w.Header().Add("Vary", "Origin")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			chatSession := r.Header.Get(constants.ChatSessionsTokenHeader)
			if chatSession == "" {
				// If the request uses API key auth (e.g. dangerousApiKey from Elements),
				// allow the requesting origin so the browser doesn't block the response.
				if r.Header.Get(constants.APIKeyHeader) != "" && isGramKeyCORSRoute(r.URL.Path) {
					w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
					w.Header().Add("Vary", "Origin")
				}
				next.ServeHTTP(w, r)
				return
			}

			claims, invalidToken, err := validator.ValidateToken(r.Context(), chatSession)
			if invalidToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if err != nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			// Validate the origin against the audience claim.
			// Browsers don't send Origin headers for same-origin GET/HEAD requests,
			// so if Origin is empty, verify the Host matches an allowed audience domain.
			origin := r.Header.Get("Origin")
			if origin != "" {
				if slices.Contains(claims.Audience, origin) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Add("Vary", "Origin")
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

			// The audience claim is the trusted-origin mechanism for Elements,
			// which is embedded on customer domains and is therefore genuinely
			// cross-site against /mcp/{slug}. Having just proven the origin
			// against that claim, exempt the request from MCPSecurity's
			// same-origin check downstream.
			next.ServeHTTP(w, markChatSessionOriginTrusted(r))
		})
	}
}

func isGramKeyCORSRoute(path string) bool {
	return slices.ContainsFunc(gramKeyCORSRoutes, func(route string) bool {
		return strings.HasPrefix(path, route)
	})
}
