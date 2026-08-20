package mcpservers

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// VerifyLiveRemoteMCPSourceInTransaction locks a remote source before creating
// an MCP server backed by it. The lock serializes source materialization with a
// concurrent soft delete, while CreateMCPServerInTransaction owns the shared
// server, issuer, and audit work.
type RemoteMCPMaterializationInput struct {
	OrganizationID    string
	ProjectID         uuid.UUID
	ActorUserID       string
	ActorEmail        string
	RemoteMCPServerID uuid.UUID
	DisplayName       string
	InitialVisibility string
}

// CreateRemoteBackedMCPServer preserves the focused remote-source command for
// callers that need its ownership lock. It delegates all shared MCP-server
// persistence, issuer, and audit work to CreateMCPServerInTransaction.
func CreateRemoteBackedMCPServer(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, input RemoteMCPMaterializationInput) (repo.McpServer, error) {
	if err := VerifyLiveRemoteMCPSourceInTransaction(ctx, tx, input.ProjectID, input.RemoteMCPServerID); err != nil {
		return repo.McpServer{}, err
	}
	return CreateMCPServerInTransaction(ctx, tx, auditLogger, MCPServerTransactionInput{
		OrganizationID:        input.OrganizationID,
		ProjectID:             input.ProjectID,
		ActorUserID:           input.ActorUserID,
		ActorEmail:            nilIfEmpty(input.ActorEmail),
		Name:                  input.DisplayName,
		Visibility:            input.InitialVisibility,
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMCPServerID:     uuid.NullUUID{UUID: input.RemoteMCPServerID, Valid: true},
		TunneledMCPServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMCPServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
}

func nilIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func VerifyLiveRemoteMCPSourceInTransaction(ctx context.Context, tx pgx.Tx, projectID, remoteMCPServerID uuid.UUID) error {
	if tx == nil || projectID == uuid.Nil || remoteMCPServerID == uuid.Nil {
		return fmt.Errorf("invalid remote MCP source verification input")
	}

	var sourceExists bool
	err := tx.QueryRow(ctx, `
SELECT TRUE
FROM remote_mcp_servers
WHERE id = $1
  AND project_id = $2
  AND deleted IS FALSE
FOR UPDATE`, remoteMCPServerID, projectID).Scan(&sourceExists)
	if err != nil {
		return fmt.Errorf("verify remote MCP source ownership: %w", err)
	}
	return nil
}
