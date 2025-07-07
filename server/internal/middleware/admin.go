package middleware

import (
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func AdminOverrideMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := r.Header.Get("Gram-Admin-Override")
		if value == "" {
			if c, err := r.Cookie("gram_admin_override"); err == nil {
				value = c.Value
			}
		}

		if value != "" {
			ctx := contextvalues.SetAdminOverrideInContext(r.Context(), value)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}
