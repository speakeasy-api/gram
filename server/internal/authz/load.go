package authz

import (
	"context"
	"fmt"

	"github.com/google/uuid"

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

// LoadKnownAgentPolicies loads direct runtime-safe policy for agent IDs that a
// caller has already resolved inside organizationID. It batches the grant read
// so candidate lists do not issue one policy query per agent.
func LoadKnownAgentPolicies(ctx context.Context, db accessrepo.DBTX, organizationID string, agentIDs []uuid.UUID) (map[uuid.UUID][]Grant, error) {
	if organizationID == "" {
		return nil, fmt.Errorf("organization id is required")
	}

	principalURNs := make([]string, 0, len(agentIDs))
	agentIDByPrincipal := make(map[string]uuid.UUID, len(agentIDs))
	policies := make(map[uuid.UUID][]Grant, len(agentIDs))
	for _, agentID := range agentIDs {
		if agentID == uuid.Nil {
			return nil, fmt.Errorf("agent id is required")
		}
		principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()).String()
		principalURNs = append(principalURNs, principal)
		agentIDByPrincipal[principal] = agentID
		policies[agentID] = nil
	}
	if len(principalURNs) == 0 {
		return policies, nil
	}

	rows, err := accessrepo.New(db).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{
		OrganizationID: organizationID,
		PrincipalUrns:  principalURNs,
	})
	if err != nil {
		return nil, fmt.Errorf("query agent policy grants: %w", err)
	}

	for _, row := range rows {
		principal := row.PrincipalUrn.String()
		agentID, ok := agentIDByPrincipal[principal]
		if !ok {
			continue
		}
		scope := Scope(row.Scope)
		if ValidateAgentRuntimeScope(CurrentAgentRuntimeScopeRegistryVersion, scope) != nil {
			continue
		}
		selector, err := SelectorFromRow(row.Selectors)
		if err != nil || ValidateSelector(scope, selector) != nil {
			continue
		}
		policies[agentID] = append(policies[agentID], Grant{
			PrincipalUrn: principal,
			Scope:        scope,
			Selector:     selector,
		})
	}

	return policies, nil
}
