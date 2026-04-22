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

// grantsFromOverrides builds a Grants object from parsed scope overrides.
// Scopes with no resources get wildcard access; scopes with resources get
// one grant per resource ID.
func grantsFromOverrides(overrides []RoleGrant) []Grant {
	var grants []Grant
	for _, o := range overrides {
		if len(o.Resources) == 0 {
			grants = append(grants, Grant{Scope: Scope(o.Scope), Resource: WildcardResource})
			continue
		}
		for _, r := range o.Resources {
			grants = append(grants, Grant{Scope: Scope(o.Scope), Resource: r})
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

		override := RoleGrant{Scope: scope, Resources: nil}
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
