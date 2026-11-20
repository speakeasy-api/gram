package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// hostedRegistration reads the server assignment, never the issuer inventory.
func (s *Service) hostedRegistration(ctx context.Context, org string, projectID, serverID uuid.UUID) (uuid.UUID, error) {
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: serverID, OrganizationID: org})
	if err != nil {
		return uuid.Nil, fmt.Errorf("load hosted upstream server: %w", err)
	}
	if server.ProjectID != projectID || !server.ToolsetID.Valid {
		return uuid.Nil, remotesessions.ErrRemoteSessionMisconfigured
	}
	associated, err := s.serverAssociatedClients(ctx, org, []uuid.UUID{serverID})
	if err != nil {
		return uuid.Nil, fmt.Errorf("load hosted upstream assignment: %w", err)
	}
	ids := associated[serverID]
	if len(ids) == 0 {
		if server.RequiresUpstreamAuth.Valid && server.RequiresUpstreamAuth.Bool {
			return uuid.Nil, fmt.Errorf("%w: hosted server needs an upstream registration", remotesessions.ErrRemoteSessionMisconfigured)
		}
		return uuid.Nil, nil
	}
	if len(ids) != 1 {
		return uuid.Nil, fmt.Errorf("%w: hosted server has multiple upstream registrations", remotesessions.ErrRemoteSessionMisconfigured)
	}
	for id := range ids {
		return id, nil
	}
	return uuid.Nil, remotesessions.ErrRemoteSessionMisconfigured
}

func (s *Service) hostedSignInClients(ctx context.Context, org string, projectID, serverID uuid.UUID) ([]remotesessions.Client, error) {
	id, err := s.hostedRegistration(ctx, org, projectID, serverID)
	if err != nil || id == uuid.Nil {
		return nil, err
	}
	clients, err := s.remoteChallengeMgr.ListAuthClientRegistrations(ctx, projectID, org, []uuid.UUID{id})
	if err != nil {
		return nil, fmt.Errorf("load hosted upstream registration: %w", err)
	}
	if len(clients) != 1 {
		return nil, fmt.Errorf("%w: hosted upstream registration is unavailable", remotesessions.ErrRemoteSessionMisconfigured)
	}
	return clients, nil
}

func (s *Service) hostedUpstreamTokens(ctx context.Context, attribution remotesessions.RefreshAttribution, org string, projectID, serverID, issuerID uuid.UUID, subject urn.SessionSubject, skipUnusable bool) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	id, err := s.hostedRegistration(ctx, org, projectID, serverID)
	if err != nil || id == uuid.Nil {
		return nil, err
	}
	tokens, err := s.remoteChallengeMgr.ResolveHostedRegistrationAccessTokens(ctx, remotesessions.AuthClientTokenRequest{
		ProjectID: projectID, OrganizationID: org, UserSessionIssuerID: issuerID, Subject: subject, RegistrationID: id, SkipUnusable: skipUnusable, Attribution: attribution,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve exact hosted upstream registration: %w", err)
	}
	return tokens, nil
}
