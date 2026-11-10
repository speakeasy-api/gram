package authclients

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// DetachMcpServer removes an auth client's link to an MCP server and records
// the detach in the caller's transaction. The caller takes its own locks. An
// error wrapping pgx.ErrNoRows reports a link that is not there.
func DetachMcpServer(ctx context.Context, dbtx pgx.Tx, auditLogger *audit.Logger, authCtx *contextvalues.AuthContext, client repo.McpAuthClient, serverID uuid.UUID) error {
	detached, err := repo.New(dbtx).DetachAuthClientFromServer(ctx, repo.DetachAuthClientFromServerParams{
		AuthClientID:   client.ID,
		McpServerID:    serverID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return fmt.Errorf("detach auth client from mcp server: %w", err)
	}
	if err := auditLogger.LogAuthClientDetachMcpServer(ctx, dbtx, audit.LogAuthClientMcpServerEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        detached.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		AuthClientURN:    urn.NewAuthClient(client.ID),
		AuthClientName:   client.Name,
		McpServerURN:     urn.NewMcpServer(serverID),
		McpServerName:    detached.McpServerName.String,
	}); err != nil {
		return fmt.Errorf("log auth client detach: %w", err)
	}
	return nil
}
