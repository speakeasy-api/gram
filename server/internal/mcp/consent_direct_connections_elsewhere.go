package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// withDirectConnectionsElsewhere preserves reconnect UX without treating another
// resource's grant as this endpoint's connection. The extra read is necessary:
// strict resource readers deliberately discard both sibling rows and ambiguity.
// Only a boolean crosses that boundary, never a chosen row or credential.
func (s *Service) withDirectConnectionsElsewhere(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, clients []remotesessions.Client, typed map[uuid.UUID]uuid.UUID, statuses map[uuid.UUID]remotesessions.RemoteSessionState) (map[uuid.UUID]remotesessions.RemoteSessionState, error) {
	resource := endpoint.directConsentResource()
	if resource == "" {
		return statuses, nil
	}
	byProject := map[uuid.UUID][]uuid.UUID{}
	legacy := false
	for _, client := range clients {
		if _, found := statuses[client.ID]; found {
			continue
		}
		if project, ok := typed[client.ID]; ok {
			byProject[project] = append(byProject[project], client.ID)
		} else {
			legacy = true
		}
	}
	elsewhere := map[uuid.UUID]bool{}
	if legacy {
		found, err := s.remoteChallengeMgr.RemoteSessionConnectionsElsewhere(ctx, subject, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, resource)
		if err != nil {
			return nil, fmt.Errorf("linked consent connections elsewhere: %w", err)
		}
		for id := range found {
			if _, isTyped := typed[id]; !isTyped {
				elsewhere[id] = true
			}
		}
	}
	for project, ids := range byProject {
		found, err := s.remoteChallengeMgr.AuthClientConnectionsElsewhere(ctx, subject, project, endpoint.OrganizationID, ids, resource)
		if err != nil {
			return nil, fmt.Errorf("typed consent connections elsewhere: %w", err)
		}
		for id := range found {
			elsewhere[id] = true
		}
	}
	for _, client := range clients {
		if _, found := statuses[client.ID]; found || !elsewhere[client.ID] {
			continue
		}
		if statuses == nil {
			statuses = make(map[uuid.UUID]remotesessions.RemoteSessionState)
		}
		// Synthetic, always different from this target, and not a sibling's resource.
		// No row ID, timestamps, scope, identity, validation, or lifecycle authority.
		statuses[client.ID] = remotesessions.RemoteSessionState{Status: remotesessions.RemoteSessionActive, Resource: resource + "#connected-elsewhere", AutoRefresh: true}
	}
	return statuses, nil
}
