package authz

import "fmt"

// GrantSurface is the product/API surface that owns writes for a family of
// grants. Reads can expose every grant, but writes must go through the owning
// surface so one part of the product cannot accidentally replace another
// part's authorization state.
type GrantSurface string

const (
	GrantSurfaceAccess     GrantSurface = "access"
	GrantSurfaceRiskPolicy GrantSurface = "risk_policy"
)

var scopeGrantSurfaces = map[Scope]GrantSurface{
	ScopeRoot:                    GrantSurfaceAccess,
	ScopeOrgRead:                 GrantSurfaceAccess,
	ScopeOrgBlockedRead:          GrantSurfaceAccess,
	ScopeOrgAdmin:                GrantSurfaceAccess,
	ScopeOrgBlockedAdmin:         GrantSurfaceAccess,
	ScopeProjectRead:             GrantSurfaceAccess,
	ScopeProjectBlockedRead:      GrantSurfaceAccess,
	ScopeProjectWrite:            GrantSurfaceAccess,
	ScopeProjectBlockedWrite:     GrantSurfaceAccess,
	ScopeMCPRead:                 GrantSurfaceAccess,
	ScopeMCPBlockedRead:          GrantSurfaceAccess,
	ScopeMCPWrite:                GrantSurfaceAccess,
	ScopeMCPBlockedWrite:         GrantSurfaceAccess,
	ScopeMCPConnect:              GrantSurfaceAccess,
	ScopeMCPBlockedConnect:       GrantSurfaceAccess,
	ScopeEnvironmentRead:         GrantSurfaceAccess,
	ScopeEnvironmentBlockedRead:  GrantSurfaceAccess,
	ScopeEnvironmentWrite:        GrantSurfaceAccess,
	ScopeEnvironmentBlockedWrite: GrantSurfaceAccess,
	ScopeRiskPolicyEvaluate:      GrantSurfaceRiskPolicy,
	ScopeRiskPolicyBypass:        GrantSurfaceRiskPolicy,
	ScopeChatRead:                GrantSurfaceAccess,
	ScopeTelemetryRead:           GrantSurfaceAccess,
}

// GrantSurfaceForScope returns the surface that owns writes for scope.
func GrantSurfaceForScope(scope Scope) (GrantSurface, bool) {
	surface, ok := scopeGrantSurfaces[scope]
	return surface, ok
}

// ValidateScopeGrantSurface verifies that the requested surface owns writes for
// scope.
func ValidateScopeGrantSurface(surface GrantSurface, scope Scope) error {
	owner, ok := GrantSurfaceForScope(scope)
	if !ok {
		return fmt.Errorf("scope %q is not managed by a grant surface", scope)
	}
	if owner != surface {
		return fmt.Errorf("scope %q is managed by %q grants, not %q grants", scope, owner, surface)
	}
	return nil
}

// ValidateGrantSurface verifies that every grant in a write payload belongs to
// the surface handling that write.
func ValidateGrantSurface(surface GrantSurface, grants []*RoleGrant) error {
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		if err := ValidateScopeGrantSurface(surface, Scope(grant.Scope)); err != nil {
			return err
		}
	}
	return nil
}
