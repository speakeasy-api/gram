package mcpendpoints

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) toolsetMirror(authCtx *contextvalues.AuthContext) hostedmcp.Mirror {
	return hostedmcp.Mirror{Audit: s.audit, ActorUserID: authCtx.UserID, ActorEmail: authCtx.Email}
}

// lockBackingToolsets locks the toolsets behind the given servers ahead of every
// domain, server, and endpoint lock and maps server id to backing toolset id.
func lockBackingToolsets(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, serverIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	backing := make(map[uuid.UUID]uuid.UUID, len(serverIDs))
	toolsetIDs := make([]uuid.NullUUID, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		server, err := mcpserversrepo.New(tx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
			ID:        serverID,
			ProjectID: projectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			continue
		case err != nil:
			return nil, fmt.Errorf("get mcp server: %w", err)
		}
		if server.ToolsetID.Valid {
			backing[serverID] = server.ToolsetID.UUID
			toolsetIDs = append(toolsetIDs, server.ToolsetID)
		}
	}
	if err := hostedmcp.LockToolsets(ctx, tx, projectID, toolsetIDs...); err != nil {
		return nil, fmt.Errorf("lock backing toolsets: %w", err)
	}
	return backing, nil
}

// verifyBacking refuses to mirror onto a toolset other than the one locked up
// front, which a concurrent server rewire could otherwise arrange.
func verifyBacking(ctx context.Context, logger *slog.Logger, locked mcpserversrepo.McpServer, backing map[uuid.UUID]uuid.UUID) error {
	want, ok := backing[locked.ID]
	if ok != locked.ToolsetID.Valid || (ok && want != locked.ToolsetID.UUID) {
		return oops.E(oops.CodeConflict, nil, "mcp server changed concurrently; retry the request").LogError(ctx, logger)
	}
	return nil
}

func (s *Service) syncBackingToolsetAddress(ctx context.Context, tx pgx.Tx, authCtx *contextvalues.AuthContext, serverID, toolsetID uuid.UUID) error {
	endpoints, err := repo.New(tx).ListMCPEndpointsByMCPServerID(ctx, repo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID:   *authCtx.ProjectID,
		McpServerID: serverID,
	})
	if err != nil {
		return fmt.Errorf("list mcp server endpoints: %w", err)
	}

	slug := pgtype.Text{String: "", Valid: false}
	customDomainID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if primary := PrimaryEndpoint(endpoints); primary != nil {
		slug = pgtype.Text{String: primary.Slug, Valid: true}
		customDomainID = primary.CustomDomainID
	}
	if err := s.toolsetMirror(authCtx).SetToolsetAddress(ctx, tx, *authCtx.ProjectID, toolsetID, slug, customDomainID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("mirror endpoint address onto toolset: %w", err)
	}
	return nil
}
