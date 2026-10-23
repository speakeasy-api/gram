package hostedmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/hostedmcp/hostingstate"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// IsCanonical reports whether server is the canonical wrapper of the toolset with toolsetID.
func IsCanonical(server *mcpserversrepo.McpServer, toolsetID uuid.UUID) bool {
	return server != nil && server.ID == toolsetID && server.ToolsetID.Valid && server.ToolsetID.UUID == toolsetID
}

// WithServerState returns toolset with its hosting state (enabled, public, session issuer) read from its
// canonical wrapper, which owns that state; without a canonical wrapper the toolset columns stand.
func WithServerState(toolset toolsetsrepo.Toolset, wrapper *mcpserversrepo.McpServer) toolsetsrepo.Toolset {
	if !IsCanonical(wrapper, toolset.ID) {
		return toolset
	}
	return hostingstate.ApplyServer(toolset, wrapper.Visibility, wrapper.UserSessionIssuerID)
}

// LoadServerState loads toolset's canonical wrapper and applies WithServerState; wrapper is nil when there is none.
// The toolset keeps its frozen address, which the slug fallback serves. A server that kept the toolset's id but
// no longer serves it leaves the toolset disabled.
func LoadServerState(ctx context.Context, db toolsetsrepo.DBTX, toolset toolsetsrepo.Toolset) (toolsetsrepo.Toolset, *mcpserversrepo.McpServer, error) {
	wrapper, err := mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return toolset, nil, nil
	case err != nil:
		return toolset, nil, fmt.Errorf("load canonical hosted MCP server: %w", err)
	}
	if !IsCanonical(&wrapper, toolset.ID) {
		return hostingstate.ApplyKept(toolset), nil, nil
	}
	return WithServerState(toolset, &wrapper), &wrapper, nil
}

// WithServerStates applies each toolset's hosting state and preferred address in one query (hostingstate.Apply).
func WithServerStates(ctx context.Context, db toolsetsrepo.DBTX, organizationID string, toolsets []toolsetsrepo.Toolset) ([]toolsetsrepo.Toolset, error) {
	ids := make([]uuid.UUID, len(toolsets))
	for i, toolset := range toolsets {
		ids[i] = toolset.ID
	}
	byID, err := hostingstate.Load(ctx, db, organizationID, ids)
	if err != nil {
		return nil, err //nolint:wrapcheck // hostingstate names the failing query.
	}
	result := make([]toolsetsrepo.Toolset, len(toolsets))
	for i, toolset := range toolsets {
		result[i] = hostingstate.Apply(toolset, hostingstate.For(byID, toolset.ID))
	}
	return result, nil
}
