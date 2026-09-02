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

// MCPServerTransactionInput contains the server-owned values needed to create
// an MCP server in a caller-owned transaction. Callers select and authorize the
// backing resource before entering this shared creation command.
type MCPServerTransactionInput struct {
	OrganizationID        string
	ProjectID             uuid.UUID
	ActorUserID           string
	ActorEmail            *string
	Name                  string
	Visibility            string
	EnvironmentID         uuid.NullUUID
	RemoteMCPServerID     uuid.NullUUID
	TunneledMCPServerID   uuid.NullUUID
	ToolsetID             uuid.NullUUID
	UnproxiedMCPServerID  uuid.NullUUID
	ToolVariationsGroupID uuid.NullUUID
}

// CreateMCPServerInTransaction creates the MCP server, its required lifetime
// issuer, and its audit event together. Both the resource-level MCP-server
// workflow and remote provisioning use this command so those invariants cannot
// drift.
func CreateMCPServerInTransaction(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, input MCPServerTransactionInput) (repo.McpServer, error) {
	if tx == nil || auditLogger == nil || input.ProjectID == uuid.Nil || input.Name == "" || input.Visibility == "" {
		return repo.McpServer{}, fmt.Errorf("invalid MCP server transaction input")
	}

	serverID, err := uuid.NewV7()
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("generate MCP server ID: %w", err)
	}
	slug, err := ComputeServerSlug(input.Name, serverID)
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("compute MCP server slug: %w", err)
	}

	issuerID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if input.RemoteMCPServerID.Valid || input.TunneledMCPServerID.Valid {
		issuerID, err = MintServerUserSessionIssuer(ctx, tx, input.OrganizationID, input.ProjectID, slug)
		if err != nil {
			return repo.McpServer{}, fmt.Errorf("mint MCP server issuer: %w", err)
		}
	}

	server, err := repo.New(tx).CreateMCPServer(ctx, repo.CreateMCPServerParams{
		ID:                    serverID,
		ProjectID:             input.ProjectID,
		Name:                  conv.ToPGText(input.Name),
		Slug:                  conv.ToPGText(slug),
		EnvironmentID:         input.EnvironmentID,
		UserSessionIssuerID:   issuerID,
		RemoteMcpServerID:     input.RemoteMCPServerID,
		TunneledMcpServerID:   input.TunneledMCPServerID,
		ToolsetID:             input.ToolsetID,
		UnproxiedMcpServerID:  input.UnproxiedMCPServerID,
		ToolVariationsGroupID: input.ToolVariationsGroupID,
		Visibility:            input.Visibility,
	})
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("create MCP server: %w", err)
	}

	if err := auditLogger.LogMcpServerCreate(ctx, tx, audit.LogMcpServerCreateEvent{
		OrganizationID:   input.OrganizationID,
		ProjectID:        input.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, input.ActorUserID),
		ActorDisplayName: input.ActorEmail,
		ActorSlug:        nil,
		McpServerURN:     urn.NewMcpServer(server.ID),
		McpServerName:    input.Name,
		McpServerSlug:    slug,
	}); err != nil {
		return repo.McpServer{}, fmt.Errorf("audit MCP server creation: %w", err)
	}

	return server, nil
}
