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
		})
	}

	defaults, err := systemRoleDefaults(ctx, db, principals, grantRows)
	if err != nil {
		return nil, err
	}

	return withSystemRoleDefaults(grantRows, defaults), nil
}

// systemRoleDefaults supplies historical assistant capabilities absent from
// persisted built-in-role grants. Creation eligibility is persisted so removing
// it from a role remains effective. Custom roles and users never gain defaults.
func systemRoleDefaults(ctx context.Context, db accessrepo.DBTX, principals []urn.Principal, grants []Grant) (map[string][]Scope, error) {
	missing := make(map[string]struct{})
	for _, principal := range principals {
		if principal.Type != urn.PrincipalTypeRole || !strings.HasPrefix(principal.ID, "global:") {
			continue
		}
		key := principal.String()
		hasAssistant := slices.ContainsFunc(grants, func(grant Grant) bool {
			return grant.PrincipalUrn == key && ResourceKindForScope(grant.Scope) == ResourceKindAssistant
		})
		if !hasAssistant {
			missing[key] = struct{}{}
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
		if _, isMissing := missing[key]; !ok || !isMissing || role.Deleted || role.WorkosDeleted {
			continue
		}
		for _, grant := range roleGrants {
			scope := Scope(grant.Scope)
			isDefault := ResourceKindForScope(scope) == ResourceKindAssistant
			alreadyPresent := slices.ContainsFunc(grants, func(existing Grant) bool {
				return existing.PrincipalUrn == key && (existing.Scope == scope || (ResourceKindForScope(scope) == ResourceKindAssistant && ResourceKindForScope(existing.Scope) == ResourceKindAssistant))
			})
			if isDefault && !alreadyPresent {
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
