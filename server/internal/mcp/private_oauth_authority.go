package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// ValidateRemoteLoginPrivateAuthority re-resolves a private endpoint before a
// remote-login callback exchanges an upstream authorization code.
func ValidateRemoteLoginPrivateAuthority(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, state remotesessions.RemoteLoginState) error {
	if !state.Authority.IsPrivate() {
		return nil
	}
	result, err := mcpendpoints.Resolve(ctx, db, logger, mcpendpoints.ResolutionInput{
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
