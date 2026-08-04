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

	return withAssistantSystemRoleDefaults(grantRows, principals), nil
}

func withAssistantSystemRoleDefaults(grants []Grant, principals []urn.Principal) []Grant {
	scopesByRole := map[string][]Scope{
		SystemRoleAdmin:  {ScopeAssistantRead, ScopeAssistantWrite},
		SystemRoleMember: {ScopeAssistantRead},
	}

	for _, principal := range principals {
		if principal.Type != urn.PrincipalTypeRole {
			continue
		}
		scopes, ok := scopesByRole[principal.ID]
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
