package authclients

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) setHostedNoUpstream(ctx context.Context, logger *slog.Logger, auth *contextvalues.AuthContext, projectID, serverID uuid.UUID) (*types.McpServerAuthentication, error) {
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, serverID.String(), projectID.String())); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin hosted authentication update").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	// Attach and detach take the same server row lock before writing placement.
	server, err := mcpserversrepo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !server.ToolsetID.Valid) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock hosted server").LogError(ctx, logger)
	}
	if err := setHostedNoUpstream(ctx, tx, auth.ActiveOrganizationID, projectID, serverID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, nil, "Detach the server's assigned registration before selecting no upstream authentication.").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "save hosted authentication setup").LogError(ctx, logger)
	}
	if !server.RequiresUpstreamAuth.Valid || server.RequiresUpstreamAuth.Bool {
		var before *bool
		if server.RequiresUpstreamAuth.Valid {
			before = &server.RequiresUpstreamAuth.Bool
		}
		if err := s.audit.LogHostedNoUpstreamAuthentication(ctx, tx, audit.LogHostedNoUpstreamAuthenticationEvent{
			OrganizationID: auth.ActiveOrganizationID, ProjectID: projectID,
			Actor: urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID), ActorDisplayName: auth.Email,
			ServerID: serverID, ServerName: conv.FromPGTextOrEmpty[string](server.Name), PreviousRequirement: before,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "audit hosted authentication update").LogError(ctx, logger)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit hosted authentication setup").LogError(ctx, logger)
	}
	return &types.McpServerAuthentication{
		McpServerID: serverID.String(), ProjectID: projectID.String(), Public: server.Visibility == "public",
		RequiresUpstreamAuth: false, Status: StatusNoAuthRequired, Clients: []*types.AuthClient{},
	}, nil
}

func setHostedNoUpstream(ctx context.Context, tx pgx.Tx, org string, project, server uuid.UUID) error {
	var id uuid.UUID
	// Do not require or remove an issuer link. Refuse any existing placement,
	// including a broken one, rather than changing its intent behind its back.
	if err := tx.QueryRow(ctx, `
UPDATE mcp_servers AS s
SET requires_upstream_auth = FALSE, updated_at = clock_timestamp()
FROM projects AS p, organization_metadata AS org
WHERE s.id = $1 AND s.project_id = $2 AND s.toolset_id IS NOT NULL AND NOT s.deleted
  AND p.id = s.project_id AND p.organization_id = $3 AND NOT p.deleted
  AND org.id = p.organization_id AND org.disabled_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM mcp_server_remote_session_clients AS a
    WHERE a.mcp_server_id = s.id AND a.project_id = p.id
      AND a.organization_id = org.id AND NOT a.deleted
  )
RETURNING s.id`, server, project, org).Scan(&id); err != nil {
		return fmt.Errorf("update hosted upstream intent: %w", err)
	}
	return nil
}
