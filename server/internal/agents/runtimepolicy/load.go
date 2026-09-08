package runtimepolicy

import (
	"context"
	"fmt"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LoadAgentPolicy resolves one exact tenant-scoped agent principal and loads its
// allow-only, agent-runtime-safe direct policy. It deliberately does not add
// user:all, owner, role, email, or WorkOS-backed principals. Invalid or unsafe
// rows fail closed independently so unrelated valid grants remain usable.
func LoadAgentPolicy(ctx context.Context, db accessrepo.DBTX, organizationID string, principal urn.Principal) ([]authz.Grant, error) {
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

	grants := make([]authz.Grant, 0, len(rows))
	for _, row := range rows {
		scope := authz.Scope(row.Scope)
		if ValidateRuntimeScope(CurrentRuntimeScopeRegistryVersion, scope) != nil {
			continue
		}
		selector, err := authz.SelectorFromRow(row.Selectors)
		if err != nil || authz.ValidateSelector(scope, selector) != nil {
			continue
		}
		grants = append(grants, authz.Grant{
			PrincipalUrn: canonical.String(),
			Scope:        scope,
			Selector:     selector,
		})
	}

	return grants, nil
}
