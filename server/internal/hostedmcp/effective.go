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

// ServerHosting is toolset's hosting state (enabled, public, session issuer) read from its canonical
// wrapper, which owns that state; a toolset without one is not hosted.
func ServerHosting(toolset toolsetsrepo.Toolset, wrapper *mcpserversrepo.McpServer) hostingstate.Hosting {
	if !IsCanonical(wrapper, toolset.ID) {
		return hostingstate.NotHosted()
	}
	return hostingstate.FromServer(toolset.ExternalOauthServerID, wrapper.Visibility, wrapper.UserSessionIssuerID)
}

// LoadServerState loads toolset's canonical wrapper and returns its ServerHosting hosting; wrapper is nil when
// there is none, and the toolset is then not hosted. That includes a server that kept the toolset's id but left it.
func LoadServerState(ctx context.Context, db toolsetsrepo.DBTX, toolset toolsetsrepo.Toolset) (hostingstate.Hosting, *mcpserversrepo.McpServer, error) {
	wrapper, err := mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return hostingstate.NotHosted(), nil, nil
	case err != nil:
		return hostingstate.NotHosted(), nil, fmt.Errorf("load canonical hosted MCP server: %w", err)
	}
	if !IsCanonical(&wrapper, toolset.ID) {
		return hostingstate.NotHosted(), nil, nil
	}
	return ServerHosting(toolset, &wrapper), &wrapper, nil
}
