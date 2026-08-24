package platformmcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// postgresDrilldownAuditor records exact user-MCP diagnoses to the shared audit
// log.
type postgresDrilldownAuditor struct {
	db *pgxpool.Pool
}

// NewPostgresDrilldownAuditor records the sensitive drill-down against the
// shared audit log.
func NewPostgresDrilldownAuditor(db *pgxpool.Pool) DrilldownAuditor {
	return &postgresDrilldownAuditor{db: db}
}

func (a *postgresDrilldownAuditor) RecordUserMCPStatusRead(ctx context.Context, principal Principal, projectID, mcpID, maskedIdentity, window string) error {
	if a == nil || a.db == nil {
		return fmt.Errorf("platform mcp drilldown auditor unavailable")
	}
	project, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf("parse audited project id: %w", err)
	}
	mcpServer, err := uuid.Parse(mcpID)
	if err != nil {
		return fmt.Errorf("parse audited mcp id: %w", err)
	}
	if err := audit.NewLogger().LogPlatformMcpDiagnosticsUserStatusRead(ctx, a.db, audit.LogPlatformMcpDiagnosticsUserStatusReadEvent{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project,
		Actor:          urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
		McpServerURN:   urn.NewMcpServer(mcpServer),
		MaskedIdentity: maskedIdentity,
		Window:         window,
	}); err != nil {
		return fmt.Errorf("record platform mcp user status audit event: %w", err)
	}
	return nil
}
