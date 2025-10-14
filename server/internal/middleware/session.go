package middleware

import (
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("gram_session")
		if err == nil {
			ctx := contextvalues.SetSessionTokenInContext(r.Context(), cookie.Value)
			r = r.WithContext(ctx)
		}
		// TODO: Remove after 11/14/25 (this is only required while we have lingering cookies)
		if r.URL.Path == "/rpc/auth.info" {
			http.SetCookie(w, &http.Cookie{
				Name:     "gram_session",
				Value:    "",
				Path:     "/rpc",
				MaxAge:   -1,
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteStrictMode,
			})
		}
		next.ServeHTTP(w, r)
	})
}
