package middleware

import (
	"net/http"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// ChatSessionMiddleware validates the chat session token and the request origin is in the allowed origins
// TODO: only allow this on certain routes?
func ChatSessionMiddleware(chatSessionsManager *chatsessions.Manager) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer next.ServeHTTP(w, r)

			chatSession := r.Header.Get(constants.ChatSessionsTokenHeader)
			if chatSession == "" {
				return
			}

			claims, err := chatSessionsManager.ValidateToken(r.Context(), chatSession)
			if err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			// If the request origin is in the allowed origins, set the allowed origin in the context to be used in the CORS middleware
			if slices.Contains(claims.AllowedOrigins, r.Header.Get("Origin")) {
				ctx := contextvalues.SetChatSessionAllowedOriginInContext(r.Context(), r.Header.Get("Origin"))
				r = r.WithContext(ctx)
			}
		})
	}
}
