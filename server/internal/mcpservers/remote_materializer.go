package mcpservers

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RemoteMCPMaterializationInput contains the server-owned values required to
// create an MCP server backed by a newly created remote MCP source. Callers
// must select the policy and authorize the actor before entering the command.
type RemoteMCPMaterializationInput struct {
	OrganizationID    string
	ProjectID         uuid.UUID
	ActorUserID       string
	ActorEmail        string
	RemoteMCPServerID uuid.UUID
	DisplayName       string
	InitialVisibility string
}

// CreateRemoteBackedMCPServer materializes the MCP-server and lifetime issuer
// for a remote source in the caller's existing transaction. Keeping the source,
// issuer, MCP server, and audit records together prevents the dashboard and
// future Platform adapters from having to coordinate rollback themselves.
func CreateRemoteBackedMCPServer(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, input RemoteMCPMaterializationInput) (repo.McpServer, error) {
	if tx == nil || auditLogger == nil || input.ProjectID == uuid.Nil || input.RemoteMCPServerID == uuid.Nil || input.DisplayName == "" || input.InitialVisibility == "" {
		return repo.McpServer{}, fmt.Errorf("invalid remote MCP materialization input")
	}

	var sourceExists bool
	err := tx.QueryRow(ctx, `
SELECT TRUE
FROM remote_mcp_servers
WHERE id = $1
  AND project_id = $2
  AND deleted IS FALSE
FOR UPDATE`, input.RemoteMCPServerID, input.ProjectID).Scan(&sourceExists)
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("verify remote MCP source ownership: %w", err)
	}

	serverID, err := uuid.NewV7()
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("generate MCP server ID: %w", err)
	}
	slug, err := computeServerSlug(input.DisplayName, serverID)
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("compute MCP server slug: %w", err)
	}
	issuerID, err := mintServerUserSessionIssuer(ctx, tx, input.ProjectID, slug)
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("mint MCP server issuer: %w", err)
	}

	server, err := repo.New(tx).CreateMCPServer(ctx, repo.CreateMCPServerParams{
		ID:                    serverID,
		ProjectID:             input.ProjectID,
		Name:                  conv.ToPGText(input.DisplayName),
		Slug:                  conv.ToPGText(slug),
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   issuerID,
		RemoteMcpServerID:     uuid.NullUUID{UUID: input.RemoteMCPServerID, Valid: true},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            input.InitialVisibility,
	})
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("create remote-backed MCP server: %w", err)
	}

	if err := auditLogger.LogMcpServerCreate(ctx, tx, audit.LogMcpServerCreateEvent{
		OrganizationID:   input.OrganizationID,
		ProjectID:        input.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, input.ActorUserID),
		ActorDisplayName: conv.PtrEmpty(input.ActorEmail),
		ActorSlug:        nil,
		McpServerURN:     urn.NewMcpServer(server.ID),
		McpServerName:    input.DisplayName,
		McpServerSlug:    slug,
	}); err != nil {
		return repo.McpServer{}, fmt.Errorf("audit remote-backed MCP server creation: %w", err)
	}

	return server, nil
}
