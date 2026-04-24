package authz

import (
	"context"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// readScopeOverrides reads the raw override header from context and parses it
// into structured overrides. Returns nil, false if no override is present.
// Each override is represented as a RoleGrant since the shape is identical.
func readScopeOverrides(ctx context.Context) ([]RoleGrant, bool) {
	raw, ok := contextvalues.GetRBACScopeOverride(ctx)
	if !ok {
		return nil, false
	}
	overrides := parseOverrideHeader(raw)
	return overrides, len(overrides) > 0
}

// GrantsFromOverrides builds a Grants slice from parsed scope overrides.
// Scopes with no selectors get wildcard access; scopes with selectors get
// one grant per selector. For backward compatibility with the header format,
// bare resource IDs are converted to selectors via NewSelector.
func GrantsFromOverrides(overrides []RoleGrant) []Grant {
	var grants []Grant
	for _, o := range overrides {
		if len(o.Selectors) == 0 {
			grants = append(grants, NewGrant(Scope(o.Scope), WildcardResource))
			continue
		}
		for _, sel := range o.Selectors {
			grants = append(grants, NewGrantWithSelector(Scope(o.Scope), sel))
		}
	}
	return grants
}

func parseOverrideHeader(value string) []RoleGrant {
	parts := strings.Split(value, ",")
	overrides := make([]RoleGrant, 0, len(parts))
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

		override := RoleGrant{Scope: scope, Selectors: nil}
		if hasResources && resourcesStr != "" {
			for r := range strings.SplitSeq(resourcesStr, "|") {
				r = strings.TrimSpace(r)
				if r != "" {
					override.Selectors = append(override.Selectors, NewSelector(Scope(scope), r))
				}
			}
		}
		overrides = append(overrides, override)
	}
	return overrides
}
