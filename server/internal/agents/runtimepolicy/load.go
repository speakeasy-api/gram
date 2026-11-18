package runtimepolicy

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LoadAgentPolicy resolves one exact tenant-scoped agent principal and loads its
// agent-runtime-safe policy and MCP restrictions: its own grants plus the grants
// of the roles it has been assigned. It deliberately does not add user:all,
// owner, email, or WorkOS-backed principals — only roles the agent itself holds.
// Invalid or unsafe rows fail closed independently so unrelated valid grants
// remain usable, which is also what keeps a role from handing an agent a scope
// its own policy could never hold.
func LoadAgentPolicy(ctx context.Context, db accessrepo.DBTX, organizationID string, principal urn.Principal) ([]authz.Grant, error) {
	agent, err := agents.ResolvePrincipal(ctx, db, organizationID, principal)
	if err != nil {
		return nil, fmt.Errorf("resolve agent policy principal: %w", err)
	}

	queries := accessrepo.New(db)
	canonical := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	rolePrincipals, err := queries.ListAgentRolePrincipals(ctx, accessrepo.ListAgentRolePrincipalsParams{
		OrganizationID: organizationID,
		AgentID:        agent.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("query agent role principals: %w", err)
	}

	principalURNs := make([]string, 0, len(rolePrincipals)+1)
	principalURNs = append(principalURNs, canonical.String())
	principalURNs = append(principalURNs, rolePrincipals...)

	rows, err := queries.GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{
		OrganizationID: organizationID,
		PrincipalUrns:  principalURNs,
	})
	if err != nil {
		return nil, fmt.Errorf("query agent policy grants: %w", err)
	}

	grants := make([]authz.Grant, 0, len(rows))
	for _, row := range rows {
		if grant, ok := normalizeAgentPolicyGrant(row); ok {
			grants = append(grants, grant)
		}
	}
	if !hasOrganizationRead(grants) {
		return grants, nil
	}

	managed, err := managedAssistantAgents(ctx, queries, organizationID, []uuid.UUID{agent.ID})
	if err != nil {
		return nil, err
	}
	if _, ok := managed[agent.ID]; ok {
		return grants, nil
	}
	return withoutOrganizationRead(grants), nil
}

// LoadKnownAgentPolicies loads direct runtime-safe policy for agent IDs that a
// caller has already resolved inside organizationID. It batches the grant read
// so candidate lists do not issue one policy query per agent.
func LoadKnownAgentPolicies(ctx context.Context, db accessrepo.DBTX, organizationID string, agentIDs []uuid.UUID) (map[uuid.UUID][]authz.Grant, error) {
	if organizationID == "" {
		return nil, fmt.Errorf("organization id is required")
	}

	principalURNs := make([]string, 0, len(agentIDs))
	agentIDByPrincipal := make(map[string]uuid.UUID, len(agentIDs))
	policies := make(map[uuid.UUID][]authz.Grant, len(agentIDs))
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

	queries := accessrepo.New(db)

	// Role membership widens each agent's policy the same way it does in
	// LoadAgentPolicy. Assignments are read for the whole organization in one
	// query and narrowed to the requested agents here, so a candidate list
	// still costs one assignment read rather than one per agent.
	assignments, err := queries.ListAgentRoleAssignments(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("query agent role assignments: %w", err)
	}
	agentIDsByRolePrincipal := make(map[string][]uuid.UUID, len(assignments))
	for _, assignment := range assignments {
		if _, requested := policies[assignment.AgentID]; !requested {
			continue
		}
		if _, seen := agentIDsByRolePrincipal[assignment.PrincipalUrn]; !seen {
			principalURNs = append(principalURNs, assignment.PrincipalUrn)
		}
		agentIDsByRolePrincipal[assignment.PrincipalUrn] = append(agentIDsByRolePrincipal[assignment.PrincipalUrn], assignment.AgentID)
	}

	rows, err := queries.GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{
		OrganizationID: organizationID,
		PrincipalUrns:  principalURNs,
	})
	if err != nil {
		return nil, fmt.Errorf("query agent policy grants: %w", err)
	}

	for _, row := range rows {
		grant, valid := normalizeAgentPolicyGrant(row)
		if !valid {
			continue
		}
		if agentID, known := agentIDByPrincipal[row.PrincipalUrn.String()]; known {
			policies[agentID] = append(policies[agentID], grant)
			continue
		}
		for _, agentID := range agentIDsByRolePrincipal[row.PrincipalUrn.String()] {
			policies[agentID] = append(policies[agentID], grant)
		}
	}

	// org:read is only admitted for agents currently bound to a project's
	// managed assistant; one batched read covers every candidate that holds it.
	orgReaders := make([]uuid.UUID, 0)
	for agentID, grants := range policies {
		if hasOrganizationRead(grants) {
			orgReaders = append(orgReaders, agentID)
		}
	}
	if len(orgReaders) == 0 {
		return policies, nil
	}
	managed, err := managedAssistantAgents(ctx, queries, organizationID, orgReaders)
	if err != nil {
		return nil, err
	}
	for _, agentID := range orgReaders {
		if _, ok := managed[agentID]; !ok {
			policies[agentID] = withoutOrganizationRead(policies[agentID])
		}
	}

	return policies, nil
}

// managedAssistantAgents returns the subset of agentIDs currently bound to a
// live project managed assistant in organizationID.
func managedAssistantAgents(ctx context.Context, queries *accessrepo.Queries, organizationID string, agentIDs []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	ids, err := queries.ListManagedAssistantAgentIDs(ctx, accessrepo.ListManagedAssistantAgentIDsParams{
		OrganizationID: organizationID,
		AgentIds:       agentIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("query managed assistant agents: %w", err)
	}
	managed := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		managed[id] = struct{}{}
	}
	return managed, nil
}

// IsManagedAssistantOnlyScope reports whether only agents bound to a project's
// managed assistant keep scope in their runtime policy. Every other agent has
// it filtered out when its policy loads, so roles cannot extend it to agents.
func IsManagedAssistantOnlyScope(scope authz.Scope) bool {
	return scope == authz.ScopeOrgRead
}

func hasOrganizationRead(grants []authz.Grant) bool {
	for _, grant := range grants {
		if grant.Scope == authz.ScopeOrgRead {
			return true
		}
	}
	return false
}

// withoutOrganizationRead drops org:read allows. MCP exclusions and other
// restrictions are untouched, so they keep narrowing the remaining policy.
func withoutOrganizationRead(grants []authz.Grant) []authz.Grant {
	filtered := make([]authz.Grant, 0, len(grants))
	for _, grant := range grants {
		if grant.Scope != authz.ScopeOrgRead {
			filtered = append(filtered, grant)
		}
	}
	return filtered
}

func normalizeAgentPolicyGrant(row accessrepo.GetPrincipalGrantsRow) (authz.Grant, bool) {
	var invalid authz.Grant
	scope := authz.Scope(row.Scope)
	// MCP exclusions restrict live parent policy; they never become delegated
	// allow grants. Keep them so broad direct or role allows can be revoked
	// on one server, or for one auth client on every server using it, without
	// disabling the agent everywhere.
	isMCPRestriction := scope == authz.ScopeMCPBlockedConnect || scope == authz.ScopeMCPBlockedRead || scope == authz.ScopeMCPBlockedWrite || scope == authz.ScopeMCPBlockedAuth
	if !isMCPRestriction && ValidateRuntimeScope(CurrentRuntimeScopeRegistryVersion, scope) != nil {
		return invalid, false
	}
	selector, err := authz.SelectorFromRow(row.Selectors)
	if err != nil || authz.ValidateSelector(scope, selector) != nil {
		return invalid, false
	}
	return authz.Grant{
		PrincipalUrn: row.PrincipalUrn.String(),
		Scope:        scope,
		Selector:     selector,
		Inferred:     row.Inferred,
	}, true
}
