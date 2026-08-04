package authz

import (
	"context"
	"fmt"

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
			Effect:       policyEffectFromText(row.Effect),
			Selector:     selectors,
		})
	}

	assistantDefaults, err := assistantSystemRoleDefaults(ctx, db, principals)
	if err != nil {
		return nil, err
	}

	return withAssistantSystemRoleDefaults(grantRows, principals, assistantDefaults), nil
}

func assistantSystemRoleDefaults(ctx context.Context, db accessrepo.DBTX, principals []urn.Principal) (map[string][]Scope, error) {
	hasLegacySystemRole := false
	for _, principal := range principals {
		if principal.Type == urn.PrincipalTypeRole && (principal.ID == SystemRoleAdmin || principal.ID == SystemRoleMember) {
			hasLegacySystemRole = true
			break
		}
	}
	if !hasLegacySystemRole {
		return nil, nil
	}

	defaults := make(map[string][]Scope, 2)
	q := accessrepo.New(db)
	for roleSlug, scopes := range map[string][]Scope{
		SystemRoleAdmin:  {ScopeAssistantRead, ScopeAssistantWrite},
		SystemRoleMember: {ScopeAssistantRead},
	} {
		role, err := q.GetGlobalRoleBySlug(ctx, roleSlug)
		if err != nil {
			return nil, fmt.Errorf("load %s system role: %w", roleSlug, err)
		}
		principal := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+role.ID.String())
		defaults[principal.String()] = scopes
	}

	return defaults, nil
}

func withAssistantSystemRoleDefaults(grants []Grant, principals []urn.Principal, scopesByPrincipal map[string][]Scope) []Grant {
	for _, principal := range principals {
		scopes, ok := scopesByPrincipal[principal.String()]
		if !ok {
			continue
		}
		for _, scope := range scopes {
			found := false
			for _, grant := range grants {
				if grant.Scope == scope && grant.Effect == PolicyEffectAllow && grant.Selector[SelectorKeyResourceKind] == ResourceKindAssistant && grant.Selector[SelectorKeyResourceID] == WildcardResource {
					found = true
					break
				}
			}
			if found {
				continue
			}
			grant := NewGrant(scope, WildcardResource)
			grant.PrincipalUrn = principal.String()
			grants = append(grants, grant)
		}
	}

	return grants
}
