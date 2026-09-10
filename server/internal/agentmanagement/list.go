package agentmanagement

import (
	"context"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
)

// List uses the same independent grant-or-ownership decisions as selected-agent
// reads. Runtime eligibility is not a management filter: suspended and revoked
// agents must remain visible so humans can inspect and manage them.
func (s *Service) List(ctx context.Context, _ *gen.ListPayload) ([]*gen.ManagedAgent, error) {
	human, err := s.authorizer.RequireHuman(ctx, s.db)
	if err != nil {
		return nil, s.serviceError(ctx, err, "list managed agents")
	}
	rows, err := repo.New(s.db).ListManagedAgents(ctx, human.Auth.ActiveOrganizationID)
	if err != nil {
		return nil, s.serviceError(ctx, fmt.Errorf("list managed agents: %w", err), "list managed agents")
	}
	readable := make([]repo.Agent, 0, len(rows))
	permissionsByID := make(map[string]AgentPermissions, len(rows))
	ownerIDs := make([]string, 0, len(rows))
	seenOwners := make(map[string]bool)
	for _, agent := range rows {
		permissions, err := s.authorizer.Permissions(ctx, human, agent)
		if err != nil {
			return nil, s.serviceError(ctx, err, "evaluate agent permissions")
		}
		if !permissions.Read {
			continue
		}
		readable = append(readable, agent)
		permissionsByID[agent.ID.String()] = permissions
		if !seenOwners[agent.OwnerUserID] {
			seenOwners[agent.OwnerUserID] = true
			ownerIDs = append(ownerIDs, agent.OwnerUserID)
		}
	}
	// Fetch profiles only for readable agents, scoped to this request's tenant.
	ownerProfiles := make(map[string]*gen.AgentOwnerProfile, len(ownerIDs))
	if len(ownerIDs) > 0 {
		profiles, err := repo.New(s.db).ListAgentOwnerProfiles(ctx, repo.ListAgentOwnerProfilesParams{
			OrganizationID: human.Auth.ActiveOrganizationID, OwnerUserIds: ownerIDs,
		})
		if err != nil {
			return nil, s.serviceError(ctx, err, "load agent owner profiles")
		}
		for _, profile := range profiles {
			view := &gen.AgentOwnerProfile{DisplayName: profile.DisplayName, PhotoURL: nil}
			if profile.PhotoUrl.Valid {
				view.PhotoURL = &profile.PhotoUrl.String
			}
			ownerProfiles[profile.ID] = view
		}
	}
	result := make([]*gen.ManagedAgent, 0, len(readable))
	for _, agent := range readable {
		result = append(result, managedAgentView(agent, permissionsByID[agent.ID.String()], ownerProfiles[agent.OwnerUserID]))
	}
	return result, nil
}
