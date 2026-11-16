package mcpservers

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// SetSetupGuideCompleted marks or clears the upstream setup guide on an MCP
// server. Marking it done again keeps the first completion time.
func (s *Service) SetSetupGuideCompleted(ctx context.Context, payload *gen.SetSetupGuideCompletedPayload) (*types.McpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	if _, err := s.requireServerWriteUnlocked(ctx, serverID, *authCtx.ProjectID, logger); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	existing, err := txRepo.LockMCPServerByIDAndProjectID(ctx, repo.LockMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, logger)
	}
	// Authorization keys on the locked row, which is authoritative if the
	// backing changed after the unlocked check.
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, grantResourceID(existing.ID, existing.ToolsetID), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	updated, err := txRepo.SetMCPServerSetupGuideCompleted(ctx, repo.SetMCPServerSetupGuideCompletedParams{
		Completed: payload.Completed,
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update mcp server setup guide").LogError(ctx, logger)
	}

	if err := s.audit.LogMcpServerUpdate(ctx, dbtx, audit.LogMcpServerUpdateEvent{
		OrganizationID:          authCtx.ActiveOrganizationID,
		ProjectID:               *authCtx.ProjectID,
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:        authCtx.Email,
		ActorSlug:               nil,
		McpServerURN:            urn.NewMcpServer(updated.ID),
		McpServerName:           conv.FromPGTextOrEmpty[string](updated.Name),
		McpServerSlug:           conv.FromPGTextOrEmpty[string](updated.Slug),
		McpServerSnapshotBefore: mv.BuildMcpServerView(existing),
		McpServerSnapshotAfter:  mv.BuildMcpServerView(updated),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp server setup guide update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	view, _ := s.buildMcpServerView(ctx, authCtx.ActiveOrganizationID, updated)
	return view, nil
}
