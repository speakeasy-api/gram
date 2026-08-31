package mcp

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func (s *Service) validateRemoteLoginPrivateAuthority(ctx context.Context, state remotesessions.RemoteLoginState) error {
	if !state.Authority.IsPrivate() {
		return nil
	}
	result, err := mcpendpoints.Resolve(ctx, s.db, s.logger, mcpendpoints.ResolutionInput{
		Slug:                 state.McpSlug,
		NamespaceKind:        mcpendpoints.NamespaceKind(state.Authority.NamespaceKind),
		CustomDomainID:       state.Authority.CustomDomainID,
		ExpectedOrganization: state.Authority.OrganizationID,
		Surface:              networkaccess.SurfacePrivate,
	})
	if err != nil {
		return fmt.Errorf("resolve private MCP endpoint: %w", err)
	}
	if !result.Found || !result.Allowed || result.Endpoint == nil || result.Endpoint.ProjectID != state.ProjectID {
		return fmt.Errorf("private MCP endpoint is unavailable")
	}
	switch {
	case result.Server != nil:
		if !result.Server.UserSessionIssuerID.Valid || result.Server.UserSessionIssuerID.UUID != state.UserSessionIssuerID {
			return fmt.Errorf("private MCP endpoint issuer changed")
		}
	case result.MetaServer != nil:
		if !result.MetaServer.UserSessionIssuerID.Valid || result.MetaServer.UserSessionIssuerID.UUID != state.UserSessionIssuerID {
			return fmt.Errorf("private MCP endpoint issuer changed")
		}
	default:
		return fmt.Errorf("private MCP endpoint backend is unavailable")
	}
	return nil
}
