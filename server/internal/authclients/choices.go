package authclients

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) ListMcpServerAuthClientChoices(ctx context.Context, payload *gen.ListMcpServerAuthClientChoicesPayload) (*gen.ListMcpServerAuthClientChoicesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	// Legacy API keys skip RBAC, so the server access check below would not
	// confine them.
	if mode, byKey := contextvalues.APIKeyAuthorization(ctx); byKey && mode != contextvalues.APIKeyAuthorizationModePrincipal {
		return nil, oops.E(oops.CodeForbidden, nil, "legacy API keys cannot list a server's credentials").LogWarn(ctx, logger)
	}

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogWarn(ctx, logger)
	}
	server, err := repo.New(s.db).GetOrganizationMCPServer(ctx, repo.GetOrganizationMCPServerParams{ID: serverID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, logger)
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, server.ID.String(), server.ProjectID.String())); err != nil {
		return nil, err
	}

	rows, err := upstreamclientsrepo.New(s.db).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: authCtx.ActiveOrganizationID, McpServerIds: []uuid.UUID{server.ID}})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list server auth clients").LogError(ctx, logger)
	}
	choices := make([]*types.AuthClientChoice, 0, len(rows))
	for _, row := range rows {
		if authz.IsSharedAuthClientType(row.ClientType.String) {
			choices = append(choices, &types.AuthClientChoice{ID: row.AuthClientID.UUID.String(), Name: row.AuthClientName.String, ClientType: row.ClientType.String})
		}
	}
	return &gen.ListMcpServerAuthClientChoicesResult{RequiresUpstreamAuth: conv.FromPGBool[bool](server.RequiresUpstreamAuth), Choices: choices}, nil
}
