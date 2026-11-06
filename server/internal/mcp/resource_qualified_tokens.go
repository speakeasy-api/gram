package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// resourceQualifiedRequest carries the target's upstream audience into the
// existing credential resolver. This is token selection, never permission or
// an association inferred from the registration's other servers.
func (s *Service) resourceQualifiedRequest(ctx context.Context, serverID uuid.UUID, req remotesessions.AuthClientTokenRequest) (remotesessions.AuthClientTokenRequest, error) {
	if serverID == uuid.Nil {
		return req, nil
	}
	if s.upstreamResource != nil {
		resource, err := s.upstreamResource(ctx, serverID, req.ProjectID)
		req.Resource = resource
		return req, err
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: req.ProjectID})
	if err != nil {
		return req, fmt.Errorf("load upstream resource target: %w", err)
	}
	resource, err := s.resolveUpstreamResource(ctx, s.logger, req.ProjectID, &server)
	if err != nil {
		return req, err
	}
	req.Resource = resource
	return req, nil
}

func (s *Service) resolveResourceQualifiedTokens(ctx context.Context, serverID uuid.UUID, req remotesessions.AuthClientTokenRequest) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	req, err := s.resourceQualifiedRequest(ctx, serverID, req)
	if err != nil {
		return nil, err
	}
	return s.upstreamTokenSource().ResolveAuthClientAccessTokens(ctx, req)
}

func (s *Service) resourceQualifiedNeedsReconnect(ctx context.Context, serverID uuid.UUID, req remotesessions.AuthClientTokenRequest) (bool, error) {
	req, err := s.resourceQualifiedRequest(ctx, serverID, req)
	if err != nil {
		return false, err
	}
	return s.remoteChallengeMgr.AuthClientNeedsReconnect(ctx, req)
}

// Compatibility-mode selected clients use the same audience selection while
// retaining their existing sign-in-link authorization boundary.
func (s *Service) resolveResourceQualifiedLegacyTokens(ctx context.Context, serverID uuid.UUID, req remotesessions.AuthClientTokenRequest) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	req, err := s.resourceQualifiedRequest(ctx, serverID, req)
	if err != nil {
		return nil, err
	}
	return s.upstreamTokenSource().ResolveClientAccessTokens(ctx, req.Attribution, req.ProjectID, req.OrganizationID, req.UserSessionIssuerID, req.Subject, req.RegistrationID, req.SkipUnusable, req.Resource)
}
