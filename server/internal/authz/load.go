package authz

import (
	"context"
	"fmt"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents"
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

	return grantRows, nil
}

// LoadAgentPolicy resolves one exact tenant-scoped agent principal and loads its
// allow-only, agent-runtime-safe direct policy. It deliberately does not add
// user:all, owner, role, email, or WorkOS-backed principals. Invalid or unsafe
// rows fail closed independently so unrelated valid grants remain usable.
func LoadAgentPolicy(ctx context.Context, db accessrepo.DBTX, organizationID string, principal urn.Principal) ([]Grant, error) {
	agent, err := agents.ResolvePrincipal(ctx, db, organizationID, principal)
	if err != nil {
		return nil, fmt.Errorf("resolve agent policy principal: %w", err)
	}

	canonical := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	rows, err := accessrepo.New(db).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{
		OrganizationID: organizationID,
		PrincipalUrns:  []string{canonical.String()},
	})
	if err != nil {
		return nil, fmt.Errorf("query agent policy grants: %w", err)
	}

	grants := make([]Grant, 0, len(rows))
	for _, row := range rows {
		scope := Scope(row.Scope)
		if ValidateAgentRuntimeScope(CurrentAgentRuntimeScopeRegistryVersion, scope) != nil {
			continue
		}
		selector, err := SelectorFromRow(row.Selectors)
		if err != nil || ValidateSelector(scope, selector) != nil {
			continue
		}
		grants = append(grants, Grant{
			PrincipalUrn: canonical.String(),
			Scope:        scope,
			Selector:     selector,
		})
	}

	return grants, nil
}
