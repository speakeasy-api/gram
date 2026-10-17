package agentmanagement

import (
	"context"
	"fmt"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// List uses the same independent grant-or-ownership decisions as selected-agent
// reads. Runtime eligibility is not a management filter: suspended and revoked
// agents must remain visible so humans can inspect and manage them.
func (s *Service) List(ctx context.Context, _ *gen.ListPayload) ([]*gen.ManagedAgent, error) {
	human, demoReadOnly, err := s.authorizer.RequireListReader(ctx, s.db)
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
	sessionSubjects := make([]string, 0, len(rows))
	keySubjects := make([]string, 0, len(rows))
	subjectAgents := map[string]map[string]string{"session": {}, "key": {}}
	for _, agent := range rows {
		permissions := AgentPermissions{Read: true, Write: false, Authorize: false, Transfer: false}
		if !demoReadOnly {
			permissions, err = s.authorizer.Permissions(ctx, human, agent)
			if err != nil {
				return nil, s.serviceError(ctx, err, "evaluate agent permissions")
			}
		}
		if !permissions.Read {
			continue
		}
		readable = append(readable, agent)
		permissionsByID[agent.ID.String()] = permissions
		// Credential timestamps follow ListSessions' OwnedAgentAuthorize gate,
		// independently of permission to see the registered identity.
		// DemoScopeGrants allows browsing synthetic data. Expose its aggregate
		// timestamp only, while projecting no credential-management capability.
		if permissions.Authorize || demoReadOnly {
			sessionSubject := urn.NewAgentSubject(agent.ID).String()
			keySubject := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String()
			sessionSubjects = append(sessionSubjects, sessionSubject)
			keySubjects = append(keySubjects, keySubject)
			subjectAgents["session"][sessionSubject] = agent.ID.String()
			subjectAgents["key"][keySubject] = agent.ID.String()
		}
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
	lastUseByAgent := make(map[string]time.Time, len(sessionSubjects))
	if len(sessionSubjects) > 0 {
		uses, err := repo.New(s.db).BatchManagedAgentCredentialLastUsed(ctx, repo.BatchManagedAgentCredentialLastUsedParams{
			OrganizationID: human.Auth.ActiveOrganizationID, SessionSubjects: sessionSubjects, KeySubjects: keySubjects,
		})
		if err != nil {
			return nil, s.serviceError(ctx, fmt.Errorf("load agent credential activity: %w", err), "list managed agents")
		}
		for _, use := range uses {
			id, ok := subjectAgents[use.Source][use.Subject]
			if ok && use.LastUsedAt.Valid && use.LastUsedAt.Time.After(lastUseByAgent[id]) {
				lastUseByAgent[id] = use.LastUsedAt.Time
			}
		}
	}
	result := make([]*gen.ManagedAgent, 0, len(readable))
	for _, agent := range readable {
		view := managedAgentView(agent, permissionsByID[agent.ID.String()], ownerProfiles[agent.OwnerUserID])
		if lastUse, ok := lastUseByAgent[agent.ID.String()]; ok {
			value := lastUse.UTC().Format(time.RFC3339Nano)
			view.LastCredentialUsedAt = &value
		}
		result = append(result, view)
	}
	return result, nil
}
