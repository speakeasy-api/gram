package middleware

import (
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// RBACOverrideMiddleware reads the X-Gram-Scope-Override header and stores
// the raw value on the request context. The access package is responsible for
// parsing the header into structured overrides.
//
// Only active when environment is "local" — a no-op in any other environment.
//
// Header format: comma-separated entries, each optionally with resource IDs:
//
//	X-Gram-Scope-Override: build:read=proj_1|proj_2,mcp:read,org:admin
func RBACOverrideMiddleware(environment string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if environment != "local" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			value := r.Header.Get("X-Gram-Scope-Override")
			if value != "" {
				r = r.WithContext(contextvalues.SetRBACScopeOverride(r.Context(), value))
			}
			next.ServeHTTP(w, r)
		})
	}
}
