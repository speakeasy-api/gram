package access

import (
	"context"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/middleware"
)

// ScopeOverride represents a single scope with optional resource restrictions,
// parsed from the X-Gram-Scope-Override header in local dev.
type ScopeOverride struct {
	Scope     string
	Resources []string // nil = unrestricted (wildcard)
}

// getScopeOverrides reads the raw override header from context and parses it
// into structured overrides. Returns nil, false if no override is present.
func getScopeOverrides(ctx context.Context) ([]ScopeOverride, bool) {
	raw, ok := middleware.GetRBACScopeOverrideRaw(ctx)
	if !ok {
		return nil, false
	}
	overrides := parseOverrideHeader(raw)
	return overrides, len(overrides) > 0
}

// grantsFromOverrides builds a Grants object from structured scope overrides.
// Scopes with no resources get wildcard access; scopes with resources get
// one grant per resource ID.
func grantsFromOverrides(overrides []ScopeOverride) *Grants {
	var rows []Grant
	for _, o := range overrides {
		if len(o.Resources) == 0 {
			rows = append(rows, Grant{Scope: Scope(o.Scope), Resource: WildcardResource})
			continue
		}
		for _, r := range o.Resources {
			rows = append(rows, Grant{Scope: Scope(o.Scope), Resource: r})
		}
	}
	return &Grants{rows: rows}
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

		override := ScopeOverride{Scope: scope, Resources: nil}
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
