package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// resourceQualifiedRequest carries the target's upstream audience into the
// existing credential resolver. This is token selection, never permission or
// an association inferred from the registration's other servers.
func (s *Service) resourceQualifiedRequest(ctx context.Context, serverID uuid.UUID, req remotesessions.AuthClientTokenRequest) (remotesessions.AuthClientTokenRequest, error) {
	// Direct endpoints already resolved this trusted value for the request.
	if req.Resource != "" {
		return req, nil
	}
	if serverID == uuid.Nil {
		return req, nil
	}
	if s.upstreamResource != nil {
		resource, err := s.upstreamResource(ctx, serverID, req.ProjectID)
		req.Resource = resource
		return req, err
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: req.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return req, oops.E(oops.CodeNotFound, err, "MCP server not found")
	}
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
	tokens, err := s.upstreamTokenSource().ResolveAuthClientAccessTokens(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("resolve resource-qualified upstream tokens: %w", err)
	}
	return tokens, nil
}

func (s *Service) resourceQualifiedNeedsReconnect(ctx context.Context, serverID uuid.UUID, req remotesessions.AuthClientTokenRequest) (bool, error) {
	req, err := s.resourceQualifiedRequest(ctx, serverID, req)
	if err != nil {
		return false, err
	}
	reconnect, err := s.remoteChallengeMgr.AuthClientNeedsReconnect(ctx, req)
	if err != nil {
		return false, fmt.Errorf("check resource-qualified connection: %w", err)
	}
	return reconnect, nil
}

// Compatibility-mode selected clients use the same audience selection while
// retaining their existing sign-in-link authorization boundary.
func (s *Service) resolveResourceQualifiedLegacyTokens(ctx context.Context, serverID uuid.UUID, req remotesessions.AuthClientTokenRequest) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	req, err := s.resourceQualifiedRequest(ctx, serverID, req)
	if err != nil {
		return nil, err
	}
	tokens, err := s.upstreamTokenSource().ResolveClientAccessTokens(ctx, req.Attribution, req.ProjectID, req.OrganizationID, req.UserSessionIssuerID, req.Subject, req.RegistrationID, req.SkipUnusable, req.Resource)
	if err != nil {
		return nil, fmt.Errorf("resolve resource-qualified legacy tokens: %w", err)
	}
	return tokens, nil
}
