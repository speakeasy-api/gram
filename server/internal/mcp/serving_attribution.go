package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/wide"
)

// servingServerID reports serving attribution without changing route authorization.
// Tools retain their established use of this identity for scanner context; a
// legacy fallback must never overwrite the fronting mcpServerID authorization marker.
func (p *mcpInputs) servingServerID() *uuid.UUID {
	if p.mcpServerID != nil {
		return p.mcpServerID
	}
	return p.attributionServerID
}

func (p *mcpInputs) resolveServingAttribution(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, toolsetID uuid.UUID) error {
	if p.servingServerID() == nil {
		servers, err := mcpserversrepo.New(db).ListEnabledMCPServersByToolsetID(ctx, mcpserversrepo.ListEnabledMCPServersByToolsetIDParams{
			ToolsetID: toolsetID, ProjectID: p.projectID,
		})
		if err != nil {
			return fmt.Errorf("list serving attribution candidates: %w", err)
		}
		switch len(servers) {
		case 0:
		case 1:
			p.attributionServerID = &servers[0].ID
		default:
			logger.WarnContext(ctx, "multiple enabled MCP servers wrap the legacy toolset; skipping server attribution", attr.SlogToolsetID(toolsetID.String()))
		}
	}
	if id := optionalUUIDString(p.servingServerID()); id != nil {
		wide.Push(ctx, attr.SlogMcpServerID(*id))
		trace.SpanFromContext(ctx).SetAttributes(attr.McpServerID(*id))
	}
	return nil
}
