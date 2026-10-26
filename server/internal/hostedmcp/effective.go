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

// WithServerState returns toolset's hosting state (enabled, public, session issuer) read from its canonical
// wrapper, which owns that state; without a canonical wrapper the toolset columns stand.
func WithServerState(toolset toolsetsrepo.Toolset, wrapper *mcpserversrepo.McpServer) hostingstate.Hosting {
	if !IsCanonical(wrapper, toolset.ID) {
		return hostingstate.ToolsetColumns(toolset).Hosting
	}
	return hostingstate.FromServer(toolset.ExternalOauthServerID, wrapper.Visibility, wrapper.UserSessionIssuerID)
}

// LoadServerState loads toolset's canonical wrapper and returns its WithServerState hosting; wrapper is nil when
// there is none. A server that kept the toolset's id but no longer serves it leaves the toolset disabled.
func LoadServerState(ctx context.Context, db toolsetsrepo.DBTX, toolset toolsetsrepo.Toolset) (hostingstate.Hosting, *mcpserversrepo.McpServer, error) {
	wrapper, err := mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return WithServerState(toolset, nil), nil, nil
	case err != nil:
		return hostingstate.Hosting{}, nil, fmt.Errorf("load canonical hosted MCP server: %w", err)
	}
	if !IsCanonical(&wrapper, toolset.ID) {
		return hostingstate.Kept(hostingstate.ToolsetColumns(toolset).UserSessionIssuerID), nil, nil
	}
	return WithServerState(toolset, &wrapper), &wrapper, nil
}
