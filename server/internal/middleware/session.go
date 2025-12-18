package middleware

import (
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/auth/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(constants.SessionCookie)
		if err == nil {
			ctx := contextvalues.SetSessionTokenInContext(r.Context(), cookie.Value)
			r = r.WithContext(ctx)
		}
		// Can delete this strangeness on 11/15/25 (after all these bad cookies expire)
		if strings.HasSuffix(r.URL.Path, "rpc/auth.info") {
			//nolint:exhaustruct // we only desire these fields and dont want to accidentally change behavior with some unexpected zero valu
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
