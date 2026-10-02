package access

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

// invalidateAgentAuthorityTx fences issuance before policy or membership writes.
// Role membership locks stabilize the affected set, then agent row locks are
// acquired in UUID order. Epochs and sessions are updated in two batch writes,
// rather than rescanning tenant sessions once per agent in a large role.
func invalidateAgentAuthorityTx(ctx context.Context, tx pgx.Tx, organizationID string, principals []string, agentIDs []uuid.UUID) error {
	principals = slices.Clone(principals)
	slices.Sort(principals)
	principals = slices.Compact(principals)
	agentIDs = slices.Clone(agentIDs)
	roles := make([]string, 0, len(principals))
	for _, principal := range principals {
		switch {
		case strings.HasPrefix(principal, "agent:"):
			id, err := uuid.Parse(strings.TrimPrefix(principal, "agent:"))
			if err != nil {
				return fmt.Errorf("parse authority principal: %w", err)
			}
			agentIDs = append(agentIDs, id)
		case strings.HasPrefix(principal, "role:"):
			if err := repo.New(tx).LockAgentRoleAssignments(ctx, repo.LockAgentRoleAssignmentsParams{OrganizationID: organizationID, RoleUrn: principal}); err != nil {
				return fmt.Errorf("lock authority role membership: %w", err)
			}
			roles = append(roles, principal)
		}
	}
	if len(roles) > 0 {
		assigned, err := repo.New(tx).ListAuthorityAgentIDsByRoles(ctx, repo.ListAuthorityAgentIDsByRolesParams{OrganizationID: organizationID, RoleUrns: roles})
		if err != nil {
			return fmt.Errorf("resolve authority agents: %w", err)
		}
		agentIDs = append(agentIDs, assigned...)
	}
	if len(agentIDs) == 0 {
		return nil
	}
	locked, err := repo.New(tx).LockAuthorityAgents(ctx, repo.LockAuthorityAgentsParams{OrganizationID: organizationID, AgentIds: agentIDs})
	if err != nil {
		return fmt.Errorf("lock authority agents: %w", err)
	}
	if len(locked) == 0 {
		return nil
	}
	if err := repo.New(tx).AdvanceAuthorityAgentEpochs(ctx, repo.AdvanceAuthorityAgentEpochsParams{OrganizationID: organizationID, AgentIds: locked}); err != nil {
		return fmt.Errorf("advance authority agent epochs: %w", err)
	}
	if err := repo.New(tx).RevokeAuthorityWorkloadSessions(ctx, repo.RevokeAuthorityWorkloadSessionsParams{OrganizationID: organizationID, AgentIds: locked}); err != nil {
		return fmt.Errorf("revoke authority workload sessions: %w", err)
	}
	return nil
}

// changedRoleAgentIDs only fences membership changes; retaining an agent in an
// otherwise unchanged role must not revoke that agent's existing sessions.
func changedRoleAgentIDs(before, after []string) ([]uuid.UUID, error) {
	changed := make(map[string]bool, len(before)+len(after))
	for _, id := range before {
		changed[id] = true
	}
	seen := make(map[string]bool, len(after))
	for _, id := range after {
		if seen[id] {
			continue
		}
		seen[id] = true
		changed[id] = !changed[id]
	}
	ids := make([]uuid.UUID, 0, len(changed))
	for raw, differs := range changed {
		if !differs {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse authority agent ID: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// changedAudiencePrincipalsTx compares the exact rules this save replaces.
// Inherited rules and unchanged principals do not invalidate other agents.
func changedAudiencePrincipalsTx(ctx context.Context, tx pgx.Tx, organizationID, resourceID string, proposed map[string][]authz.PrincipalSelectors) ([]string, error) {
	before, after := make(map[string][]string), make(map[string][]string)
	for level, scope := range audienceLevelScopes {
		grants, err := authz.ListGrantsForResource(ctx, tx, authz.Resource{OrganizationID: organizationID, Scope: scope, ResourceID: resourceID})
		if err != nil {
			return nil, fmt.Errorf("load authority audience: %w", err)
		}
		for _, grant := range grants {
			encoded, err := json.Marshal(grant.Selector)
			if err != nil {
				return nil, fmt.Errorf("encode existing audience selector: %w", err)
			}
			before[grant.PrincipalUrn] = append(before[grant.PrincipalUrn], string(scope)+"|"+string(encoded))
		}
		for _, entry := range proposed[level] {
			selectors := entry.Selectors
			if len(selectors) == 0 {
				selectors = []authz.Selector{authz.NewSelector(scope, resourceID)}
			}
			for _, selector := range selectors {
				encoded, err := json.Marshal(selector)
				if err != nil {
					return nil, fmt.Errorf("encode proposed audience selector: %w", err)
				}
				principal := entry.Principal.String()
				after[principal] = append(after[principal], string(scope)+"|"+string(encoded))
			}
		}
	}
	changed := make([]string, 0)
	for principal, previous := range before {
		next := after[principal]
		slices.Sort(previous)
		slices.Sort(next)
		if !slices.Equal(slices.Compact(previous), slices.Compact(next)) {
			changed = append(changed, principal)
		}
		delete(after, principal)
	}
	for principal := range after {
		changed = append(changed, principal)
	}
	return changed, nil
}
