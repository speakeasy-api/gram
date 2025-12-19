package middleware

import (
	"net/http"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

// ChatSessionMiddleware validates the chat session token and the request origin is in the allowed origins
// Attached only to routes that allow chat sessions token authentication
func ChatSessionMiddleware(chatSessionsManager *chatsessions.Manager) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
