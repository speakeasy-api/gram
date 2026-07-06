package plugins

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// AttachToDefaultPluginParams identifies the server to attach — exactly one
// of ToolsetID / McpServerID must be Valid, mirroring the plugin_servers
// backend-exclusivity constraint.
type AttachToDefaultPluginParams struct {
	OrganizationID string
	ProjectID      uuid.UUID
	ToolsetID      uuid.NullUUID
	McpServerID    uuid.NullUUID
	DisplayName    string
}

// AttachToDefaultPluginResult is nil when AttachToDefaultPlugin no-ops
// (no Default plugin for the project, or the server is already attached).
type AttachToDefaultPluginResult struct {
	PluginID   uuid.UUID
	PluginName string
	PluginSlug string
	Server     repo.PluginServer
}

// AttachToDefaultPlugin idempotently adds a server (toolset- or mcp_server-
// backed) to a project's Default plugin, so it shows up in the
// auto-published marketplace without a human visiting the Plugins page.
// Callers (toolsets, on MCP-enable; mcpendpoints, on first endpoint) run this
// in the same transaction as the triggering write. A project with no Default
// plugin (predates this feature) or a server that's already attached are
// both expected no-ops, not errors — reported by a nil result.
func AttachToDefaultPlugin(ctx context.Context, q *repo.Queries, params AttachToDefaultPluginParams) (*AttachToDefaultPluginResult, error) {
	defaultPlugin, err := q.GetDefaultPlugin(ctx, repo.GetDefaultPluginParams{
		OrganizationID: params.OrganizationID,
		ProjectID:      params.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("get default plugin: %w", err)
	}

	server, err := q.AddPluginServer(ctx, repo.AddPluginServerParams{
		PluginID:    defaultPlugin.ID,
		ToolsetID:   params.ToolsetID,
		McpServerID: params.McpServerID,
		DisplayName: params.DisplayName,
		Policy:      "required",
		SortOrder:   0,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, nil
		}
		return nil, fmt.Errorf("attach server to default plugin: %w", err)
	}

	return &AttachToDefaultPluginResult{
		PluginID:   defaultPlugin.ID,
		PluginName: defaultPlugin.Name,
		PluginSlug: defaultPlugin.Slug,
		Server:     server,
	}, nil
}
