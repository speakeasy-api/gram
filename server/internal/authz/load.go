package authz

import (
	"context"
	"fmt"
	"slices"
	"strings"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LoadGrants loads and normalizes grants for the given organization and principals.
func LoadGrants(ctx context.Context, db accessrepo.DBTX, organizationID string, principals []urn.Principal) ([]Grant, error) {
	if organizationID == "" {
		return nil, fmt.Errorf("organization id is required")
	}

	principalURNs, err := principalURNStrings(principals)
	if err != nil {
		return nil, err
	}

	rows, err := accessrepo.New(db).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{
		OrganizationID: organizationID,
		PrincipalUrns:  principalURNs,
	})
	if err != nil {
		return nil, fmt.Errorf("query principal grants: %w", err)
	}

	grantRows := make([]Grant, 0, len(rows))
	for _, row := range rows {
		selectors, err := SelectorFromRow(row.Selectors)
		if err != nil {
			return nil, fmt.Errorf("unmarshal grant selector: %w", err)
		}
		grantRows = append(grantRows, Grant{
			PrincipalUrn: row.PrincipalUrn.String(),
			Scope:        Scope(row.Scope),
			Selector:     selectors,
			Inferred:     row.Inferred,
		})
	}

	defaults, err := lateSystemRoleDefaults(ctx, db, principals, grantRows)
	if err != nil {
		return nil, err
	}

	return withSystemRoleDefaults(grantRows, defaults), nil
}

// lateDefaultGroup is a set of system-role default scopes added after
// organizations were already seeded. It is supplied at load time to a
// built-in role principal that holds no stored grant in the group, when the
// group can apply to that principal at all.
type lateDefaultGroup struct {
	// contains reports whether a scope belongs to the group.
	contains func(Scope) bool
	// appliesTo reports, from a principal's stored grants, whether its role
	// can hold the group. It keeps roles that never get the group, such as
	// member for admin-only scopes, from costing a role lookup on every load.
	appliesTo func(stored []Grant) bool
}

var lateDefaultGroups = []lateDefaultGroup{
	// Assistant scopes, defaults of every built-in role.
	{
		contains:  func(scope Scope) bool { return ResourceKindForScope(scope) == ResourceKindAssistant },
		appliesTo: func([]Grant) bool { return true },
	},
	// Organizational Memory scopes, an org:admin default.
	{
		contains: func(scope Scope) bool { return scope == ScopeMemoryRead || scope == ScopeMemoryWrite },
		appliesTo: func(stored []Grant) bool {
			return slices.ContainsFunc(stored, func(grant Grant) bool { return grant.Scope == ScopeOrgAdmin })
		},
	},
}

// lateSystemRoleDefaults returns the late-added default scopes each built-in
// role principal holds, keyed by principal. Organizations seeded before these
// scopes became defaults have no persisted rows for them, and system-role
// grants are immutable, so the defaults are supplied at load time instead. The
// global roles are only looked up when a principal lacks a group, which keeps
// the extra query off organizations seeded with the current defaults.
func lateSystemRoleDefaults(ctx context.Context, db accessrepo.DBTX, principals []urn.Principal, grants []Grant) (map[string][]Scope, error) {
	missing := make(map[string][]int)
	for _, principal := range principals {
		if principal.Type != urn.PrincipalTypeRole || !strings.HasPrefix(principal.ID, "global:") {
			continue
		}
		key := principal.String()
		var stored []Grant
		for _, grant := range grants {
			if grant.PrincipalUrn == key {
				stored = append(stored, grant)
			}
		}
		for i, group := range lateDefaultGroups {
			if group.appliesTo(stored) && !slices.ContainsFunc(stored, func(grant Grant) bool { return group.contains(grant.Scope) }) {
				missing[key] = append(missing[key], i)
			}
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}

	roles, err := accessrepo.New(db).ListGlobalRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list global system roles: %w", err)
	}
	defaults := make(map[string][]Scope, len(missing))
	for _, role := range roles {
		roleGrants, ok := SystemRoleGrants[role.WorkosSlug]
		key := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+role.ID.String()).String()
		groups, isMissing := missing[key]
		if !ok || !isMissing || role.Deleted || role.WorkosDeleted {
			continue
		}
		for _, grant := range roleGrants {
			scope := Scope(grant.Scope)
			if slices.ContainsFunc(groups, func(i int) bool { return lateDefaultGroups[i].contains(scope) }) {
				defaults[key] = append(defaults[key], scope)
			}
		}
	}
	return defaults, nil
}

func withSystemRoleDefaults(grants []Grant, scopesByPrincipal map[string][]Scope) []Grant {
	for principal, scopes := range scopesByPrincipal {
		for _, scope := range scopes {
			grant := NewGrant(scope, WildcardResource)
			grant.PrincipalUrn = principal
			grants = append(grants, grant)
		}
	}
	return grants
}
