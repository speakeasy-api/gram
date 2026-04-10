package middleware

import (
	"context"
	"net/http"
	"strings"
)

type rbacOverrideKey struct{}

// ScopeOverride represents a single scope with optional resource restrictions.
type ScopeOverride struct {
	Scope     string
	Resources []string // empty = unrestricted (wildcard)
}

// RBACOverrideMiddleware reads the X-Gram-Scope-Override header and stores
// the requested scopes on the request context. The access manager checks for
// the override before loading real grants.
//
// Only active when environment is "local" — a no-op in any other environment.
//
// Header format: comma-separated entries, each optionally with resource IDs:
//
//	X-Gram-Scope-Override: build:read=proj_1|proj_2,mcp:read,org:admin
//
// A scope without "=" gets wildcard access. A scope with "=" is restricted to
// the pipe-separated resource IDs.
func RBACOverrideMiddleware(environment string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if environment != "local" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			value := r.Header.Get("X-Gram-Scope-Override")
			if value != "" {
				overrides := parseOverrideHeader(value)
				if len(overrides) > 0 {
					ctx := context.WithValue(r.Context(), rbacOverrideKey{}, overrides)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// GetRBACScopeOverride returns the override scopes from context, if any.
func GetRBACScopeOverride(ctx context.Context) ([]ScopeOverride, bool) {
	overrides, ok := ctx.Value(rbacOverrideKey{}).([]ScopeOverride)
	return overrides, ok && len(overrides) > 0
}

func parseOverrideHeader(value string) []ScopeOverride {
	parts := strings.Split(value, ",")
	overrides := make([]ScopeOverride, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		scope, resourcesStr, hasResources := strings.Cut(part, "=")
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}

		override := ScopeOverride{Scope: scope}
		if hasResources && resourcesStr != "" {
			for r := range strings.SplitSeq(resourcesStr, "|") {
				r = strings.TrimSpace(r)
				if r != "" {
					override.Resources = append(override.Resources, r)
				}
			}
		}
		overrides = append(overrides, override)
	}
	return overrides
}
