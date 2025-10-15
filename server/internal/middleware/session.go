package middleware

import (
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("gram_session")
		if err == nil {
			ctx := contextvalues.SetSessionTokenInContext(r.Context(), cookie.Value)
			r = r.WithContext(ctx)
		}
		if strings.HasSuffix(r.URL.Path, "rpc/auth.info") {
			http.SetCookie(w, &http.Cookie{
				Name:     "gram_session",
				Value:    "",
				MaxAge:   -1,
				Path:     "/rpc",
				HttpOnly: true,
				Secure:   true,
			})
		}
		next.ServeHTTP(w, r)
	})
}
