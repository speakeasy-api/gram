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

// WithServerState returns toolset with its hosting state (enabled, public, session issuer) read from its
// canonical wrapper, which owns that state; without a canonical wrapper the toolset columns stand.
// A disabled wrapper keeps the toolset's public flag, which is all that records it.
func WithServerState(toolset toolsetsrepo.Toolset, wrapper *mcpserversrepo.McpServer) toolsetsrepo.Toolset {
	if !IsCanonical(wrapper, toolset.ID) {
		return toolset
	}
	switch wrapper.Visibility {
	case visibility.Public:
		toolset.McpEnabled, toolset.McpIsPublic = true, true
	case visibility.Private:
		toolset.McpEnabled, toolset.McpIsPublic = true, false
	default:
		toolset.McpEnabled = false
	}
	toolset.UserSessionIssuerID = wrapper.UserSessionIssuerID
	return toolset
}

// InSync reports whether a canonical wrapper still matches its toolset's hosting columns.
// Serving denies drifted pairs until the wrapper becomes the only writer.
func InSync(toolset toolsetsrepo.Toolset, wrapper *mcpserversrepo.McpServer) bool {
	return IsCanonical(wrapper, toolset.ID) && wrapper.Visibility == Visibility(toolset) && wrapper.UserSessionIssuerID == toolset.UserSessionIssuerID
}

// LoadServerState loads toolset's canonical wrapper and applies WithServerState; wrapper is nil when there is none.
// A server that kept the toolset's id but no longer serves it leaves the toolset disabled, and a drifted pair
// (see InSync) reads as disabled and private, as serving denies it.
func LoadServerState(ctx context.Context, db mcpserversrepo.DBTX, toolset toolsetsrepo.Toolset) (toolsetsrepo.Toolset, *mcpserversrepo.McpServer, error) {
	wrapper, err := mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return toolset, nil, nil
	case err != nil:
		return toolset, nil, fmt.Errorf("load canonical hosted MCP server: %w", err)
	}
	if !IsCanonical(&wrapper, toolset.ID) {
		toolset.McpEnabled = false
		return toolset, nil, nil
	}
	if !InSync(toolset, &wrapper) {
		toolset.McpEnabled, toolset.McpIsPublic = false, false
		return toolset, &wrapper, nil
	}
	return WithServerState(toolset, &wrapper), &wrapper, nil
}
