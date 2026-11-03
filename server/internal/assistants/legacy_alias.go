package assistants

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
)

// loadAssistantMcpServers loads attached MCP servers; a hosted server with no usable endpoint keeps its toolset's
// legacy /mcp/<slug> alias until the toolset slug fallback is removed.
func loadAssistantMcpServers(ctx context.Context, db repo.DBTX, params repo.LoadAssistantMcpServersParams) ([]repo.LoadAssistantMcpServersRow, error) {
	rows, err := repo.New(db).LoadAssistantMcpServers(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("load assistant mcp servers: %w", err)
	}
	var ids []uuid.UUID
	for _, row := range rows {
		if row.HostedToolset && row.EndpointSlug == "" {
			ids = append(ids, row.McpServerID)
		}
	}
	aliases, err := legacyalias.List(ctx, db, params.ProjectID, ids)
	if err != nil {
		return nil, fmt.Errorf("load assistant MCP server aliases: %w", err)
	}
	for i, row := range rows {
		if alias, ok := aliases[row.McpServerID]; ok && row.HostedToolset && row.EndpointSlug == "" {
			rows[i].EndpointSlug = alias.Slug.String
		}
	}
	return rows, nil
}

// hasLegacyAlias reports which of the toolsets carry a legacy alias.
func hasLegacyAlias(ctx context.Context, db repo.DBTX, projectID uuid.UUID, toolsetIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	aliases, err := legacyalias.List(ctx, db, projectID, toolsetIDs)
	if err != nil {
		return nil, fmt.Errorf("load assistant MCP server aliases: %w", err)
	}
	has := make(map[uuid.UUID]bool, len(aliases))
	for id := range aliases {
		has[id] = true
	}
	return has, nil
}
