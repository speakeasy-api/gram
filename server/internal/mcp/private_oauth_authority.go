package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func (s *Service) recordPrivateOAuthAuthority(ctx context.Context, authority networkingress.Authority, started time.Time, err error) {
	if !authority.IsPrivate() {
		return
	}
	result, reason := networkingress.ResultAllowed, networkingress.ReasonNone
	if err != nil {
		result, reason = networkingress.ResultDenied, networkingress.ReasonAuthorityRejected
		if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
			result, reason = networkingress.ResultError, networkingress.ReasonAuthorityUnavailable
		}
	}
	s.networkIngressTelemetry.Record(ctx, networkingress.OperationOAuthAuthority, result, reason, "unknown", time.Since(started))
}

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
		if (state.McpServerID.Valid && result.Server.ID != state.McpServerID.UUID) ||
			state.MetaMcpServerID.Valid ||
			!result.Server.UserSessionIssuerID.Valid || result.Server.UserSessionIssuerID.UUID != state.UserSessionIssuerID {
			return fmt.Errorf("private MCP endpoint backend or issuer changed")
		}
	case result.MetaServer != nil:
		if state.McpServerID.Valid ||
			(state.MetaMcpServerID.Valid && result.MetaServer.ID != state.MetaMcpServerID.UUID) ||
			!result.MetaServer.UserSessionIssuerID.Valid || result.MetaServer.UserSessionIssuerID.UUID != state.UserSessionIssuerID {
			return fmt.Errorf("private MCP endpoint backend or issuer changed")
		}
	default:
		return fmt.Errorf("private MCP endpoint backend is unavailable")
	}
	return nil
}
