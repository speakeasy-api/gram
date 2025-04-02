package middleware

import (
	"net/http"

	"github.com/speakeasy-api/gram/internal/sessions"
)

func SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("gram_session")
		if err == nil {
			ctx := sessions.SetSessionTokenInContext(r.Context(), cookie.Value)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}
