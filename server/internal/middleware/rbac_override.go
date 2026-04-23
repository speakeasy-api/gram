package middleware

import (
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// RBACOverrideMiddleware reads the X-Gram-Scope-Override header and stores
// the raw value on the request context. The access package is responsible for
// parsing the header into structured overrides and ensuring that only admins
// can use it in non-development environments.
//
// Header format: comma-separated entries, each optionally with resource IDs:
//
//	X-Gram-Scope-Override: project:read=proj_1|proj_2,mcp:read,org:admin
func RBACOverrideMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			value := r.Header.Get("X-Gram-Scope-Override")
			if value != "" {
				r = r.WithContext(contextvalues.SetRBACScopeOverride(r.Context(), value))
			}
			next.ServeHTTP(w, r)
		})
	}
}
