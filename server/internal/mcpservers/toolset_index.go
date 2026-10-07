package mcpservers

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
)

// ToolsetIndexTrigger requests the tool-search index build for a toolset that
// an MCP server has just started fronting. It returns nil both when it
// requested a build and when the toolset needs none; any error means a build
// was needed and could not be requested.
//
// Dynamic-mode tools/list refuses a toolset with no index, and a toolset only
// needs one once something serves it. A server that starts fronting a toolset
// is often what makes it served, so the toolset's own version writes cannot
// have requested the build. The server composition supplies the toolsets
// package's trigger here, which mcpservers cannot import directly.
type ToolsetIndexTrigger func(ctx context.Context, projectID, toolsetID uuid.UUID) error

// frontedToolset returns the toolset the server serves through its own
// address: a live, non-disabled row backed by a toolset, other than that
// toolset's hosted address, which is served only while the toolset is
// MCP-enabled and so never makes it served by itself.
func frontedToolset(server repo.McpServer) (uuid.UUID, bool) {
	if !server.ToolsetID.Valid || isCanonicalHostedWrapper(server) || server.Deleted || server.Visibility == VisibilityDisabled {
		return uuid.Nil, false
	}
	return server.ToolsetID.UUID, true
}

// requestToolsetIndex requests the index of the toolset after fronts when this
// write is what made after start fronting it: a new server, a server pointed at
// a different toolset, or a disabled server enabled again. before is nil for a
// create.
//
// Call it only after the write has committed. It never fails the write: the
// change already landed, and the periodic indexing sweep still reaches the
// toolset, so a failure is only logged.
func (s *Service) requestToolsetIndex(ctx context.Context, logger *slog.Logger, before *repo.McpServer, after repo.McpServer) {
	if s.toolsetIndexTrigger == nil {
		return
	}
	toolsetID, fronts := frontedToolset(after)
	if !fronts {
		return
	}
	if before != nil {
		if previous, frontedBefore := frontedToolset(*before); frontedBefore && previous == toolsetID {
			return
		}
	}
	if err := s.toolsetIndexTrigger(ctx, after.ProjectID, toolsetID); err != nil {
		logger.WarnContext(ctx, "request tool-search index for the toolset an MCP server now fronts; dynamic mode cannot list its tools until the periodic sweep builds it",
			attr.SlogMcpServerID(after.ID.String()), attr.SlogToolsetID(toolsetID.String()), attr.SlogError(err))
	}
}
