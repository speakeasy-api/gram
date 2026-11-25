package admin

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	remotemcpgen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
)

func (s *Service) SetMcpServerScopePin(ctx context.Context, payload *gen.SetMcpServerScopePinPayload) (*gen.AdminMcpServerResourceScopes, error) {
	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id")
	}
	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id")
	}

	belongs, err := repo.New(s.db).AdminProjectBelongsToOrganization(ctx, repo.AdminProjectBelongsToOrganizationParams{
		ProjectID:      projectID,
		OrganizationID: payload.OrganizationID,
	})
	switch {
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lookup project organization").LogError(ctx, s.logger, attr.SlogOrganizationID(payload.OrganizationID), attr.SlogProjectID(projectID.String()))
	case !belongs:
		return nil, oops.C(oops.CodeNotFound)
	}

	actor, displayName, _ := adminActor(ctx)
	scopes, err := s.scopes.SetPin(ctx, remotemcp.StaffScopePin{
		Target:           remotemcp.StaffScopeTarget{OrganizationID: payload.OrganizationID, ProjectID: projectID, McpServerID: serverID},
		Scopes:           payload.Scopes,
		Actor:            actor,
		ActorDisplayName: displayName,
	})
	if err != nil {
		return nil, fmt.Errorf("admin SetMcpServerScopePin: %w", err)
	}
	return adminResourceScopes(scopes), nil
}

func adminResourceScopes(scopes *remotemcpgen.RemoteMcpServerScopes) *gen.AdminMcpServerResourceScopes {
	clients := make([]*gen.AdminMcpServerResourceScopeClient, 0, len(scopes.Clients))
	for _, client := range scopes.Clients {
		clients = append(clients, &gen.AdminMcpServerResourceScopeClient{
			ClientID:                 client.ClientID,
			ScopeSource:              client.ScopeSource,
			RequestedScopes:          client.RequestedScopes,
			UnadvertisedPinnedScopes: client.UnadvertisedPinnedScopes,
			PinWouldDecide:           client.PinWouldDecide,
		})
	}
	return &gen.AdminMcpServerResourceScopes{
		ResourceURL:           scopes.ResourceURL,
		PinnedScopes:          scopes.PinnedScopes,
		AdvertisedScopesKnown: scopes.AdvertisedScopesKnown,
		AdvertisedScopes:      scopes.AdvertisedScopes,
		ChallengeScopes:       scopes.ChallengeScopes,
		SharedServerCount:     scopes.SharedServerCount,
		Clients:               clients,
	}
}
