package mcpservers

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
)

// RemoteMCPMaterializationInput names the remote source an MCP server is
// created in front of, and the server's own values.
type RemoteMCPMaterializationInput struct {
	OrganizationID    string
	ProjectID         uuid.UUID
	ActorUserID       string
	ActorEmail        string
	RemoteMCPServerID uuid.UUID
	DisplayName       string
	InitialVisibility string
}

// CreateRemoteBackedMCPServer names the remote-source shape of the shared
// creation command, which locks the source and owns all MCP-server
// persistence, issuer, and audit work.
func CreateRemoteBackedMCPServer(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, input RemoteMCPMaterializationInput) (repo.McpServer, error) {
	return CreateMCPServerInTransaction(ctx, tx, auditLogger, MCPServerTransactionInput{
		OrganizationID:        input.OrganizationID,
		ProjectID:             input.ProjectID,
		ActorUserID:           input.ActorUserID,
		ActorEmail:            nilIfEmpty(input.ActorEmail),
		Name:                  input.DisplayName,
		Visibility:            input.InitialVisibility,
		NetworkAccessMode:     networkaccess.ModePublicOnly,
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
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
