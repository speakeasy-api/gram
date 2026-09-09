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
		return nil, err
	}
	rows, err := repo.New(s.db).ListManagedAgents(ctx, human.Auth.ActiveOrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list managed agents: %w", err)
	}
	// All rows belong to the active organization. Cache absent profiles too,
	// and keep the cache request-local so membership changes are not retained.
	ownerProfiles := make(map[string]*gen.AgentOwnerProfile)
	result := make([]*gen.ManagedAgent, 0, len(rows))
	for _, agent := range rows {
		permissions, err := s.authorizer.Permissions(ctx, human, agent)
		if err != nil {
			return nil, err
		}
		if !permissions.Read {
			continue
		}
		profile, loaded := ownerProfiles[agent.OwnerUserID]
		if !loaded {
			profile, err = s.ownerProfile(ctx, agent.OrganizationID, agent.OwnerUserID)
			if err != nil {
				return nil, err
			}
			ownerProfiles[agent.OwnerUserID] = profile
		}
		result = append(result, managedAgentView(agent, permissions, profile))
	}
	return result, nil
}
