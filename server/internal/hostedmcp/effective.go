package hostedmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// IsCanonical reports whether server is the canonical wrapper of the toolset with toolsetID.
func IsCanonical(server *mcpserversrepo.McpServer, toolsetID uuid.UUID) bool {
	return server != nil && server.ID == toolsetID && server.ToolsetID.Valid && server.ToolsetID.UUID == toolsetID
}

// Effective returns toolset with its hosting state (enabled, public, session issuer) read from its
// canonical wrapper, which owns that state; without a canonical wrapper the toolset columns stand.
func Effective(toolset toolsetsrepo.Toolset, wrapper *mcpserversrepo.McpServer) toolsetsrepo.Toolset {
	if !IsCanonical(wrapper, toolset.ID) {
		return toolset
	}
	toolset.McpEnabled = wrapper.Visibility == visibility.Public || wrapper.Visibility == visibility.Private
	toolset.McpIsPublic = wrapper.Visibility == visibility.Public
	toolset.UserSessionIssuerID = wrapper.UserSessionIssuerID
	return toolset
}

// LoadEffective loads toolset's canonical wrapper and applies Effective; wrapper is nil when there is none.
func LoadEffective(ctx context.Context, db mcpserversrepo.DBTX, toolset toolsetsrepo.Toolset) (toolsetsrepo.Toolset, *mcpserversrepo.McpServer, error) {
	wrapper, err := mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return toolset, nil, nil
	case err != nil:
		return toolset, nil, fmt.Errorf("load canonical hosted MCP server: %w", err)
	}
	if !IsCanonical(&wrapper, toolset.ID) {
		return toolset, nil, nil
	}
	return Effective(toolset, &wrapper), &wrapper, nil
}
