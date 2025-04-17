package middleware

import (
	"net/http"

	"github.com/speakeasy-api/gram/internal/contextvalues"
)

func AdminOverrideMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value := r.Header.Get("Gram-Admin-Override"); value != "" {
			ctx := contextvalues.SetAdminOverrideInContext(r.Context(), value)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}
