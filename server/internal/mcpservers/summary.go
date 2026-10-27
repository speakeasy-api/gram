package mcpservers

import (
	"context"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/endpointaddr"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
)

// EnrichViews fills each view's toolset summary, primary endpoint, platform
// endpoint slug, legacy platform slug and has_endpoint in up to five queries,
// whatever the number of servers. views[i] must be servers[i]'s view. A failed
// query is logged and leaves its fields empty; endpointsResolved reports whether
// the primary and platform endpoint fields could be read.
func EnrichViews(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, serverURL *url.URL, organizationID string, servers []repo.McpServer, views []*types.McpServer) (endpointsResolved bool) {
	if len(servers) == 0 {
		return true
	}

	projectSet := make(map[uuid.UUID]struct{})
	toolsetSet := make(map[uuid.UUID]struct{})
	serverIDs := make([]uuid.UUID, len(servers))
	serverIDStrings := make([]string, len(servers))
	for i, server := range servers {
		serverIDs[i] = server.ID
		serverIDStrings[i] = server.ID.String()
		projectSet[server.ProjectID] = struct{}{}
		if server.ToolsetID.Valid {
			toolsetSet[server.ToolsetID.UUID] = struct{}{}
		}
	}
	projectIDs := make([]uuid.UUID, 0, len(projectSet))
	for id := range projectSet {
		projectIDs = append(projectIDs, id)
	}
	logger = logger.With(attr.SlogOrganizationID(organizationID), attr.SlogMcpServerIDs(serverIDStrings))

	summaries := make(map[uuid.UUID]*types.McpServerToolsetSummary, len(toolsetSet))
	if len(toolsetSet) > 0 {
		toolsetIDs := make([]uuid.UUID, 0, len(toolsetSet))
		for id := range toolsetSet {
			toolsetIDs = append(toolsetIDs, id)
		}
		rows, err := repo.New(db).ListToolsetSummariesByIDs(ctx, repo.ListToolsetSummariesByIDsParams{
			ToolsetIds:     toolsetIDs,
			OrganizationID: organizationID,
			ProjectIds:     projectIDs,
		})
		if err != nil {
			logger.WarnContext(ctx, "list mcp server toolset summaries", attr.SlogError(err))
		}
		for _, row := range rows {
			summaries[row.ID] = mv.BuildMcpServerToolsetSummaryView(row)
		}
	}
	if len(summaries) > 0 {
		toolsetIDs := make([]uuid.UUID, 0, len(summaries))
		for id := range summaries {
			toolsetIDs = append(toolsetIDs, id)
		}
		// Kept apart from the summary query so a failed endpoint read never drops a summary.
		hosts, err := repo.New(db).ListToolsetHostingMCPServerIDs(ctx, repo.ListToolsetHostingMCPServerIDsParams{
			OrganizationID: organizationID,
			ToolsetIds:     toolsetIDs,
			ProjectIds:     projectIDs,
		})
		if err != nil {
			logger.WarnContext(ctx, "list toolset hosting mcp servers", attr.SlogError(err))
		}
		for _, host := range hosts {
			if summary, ok := summaries[host.ToolsetID]; ok {
				summary.HostingMcpServerID = new(host.McpServerID.String())
			}
		}
	}

	base := ""
	if serverURL != nil {
		base = serverURL.String()
	}
	primaries, err := endpointaddr.PrimaryEndpointsByServer(ctx, db, organizationID, projectIDs, serverIDs, base)
	if err != nil {
		logger.WarnContext(ctx, "list mcp server primary endpoints", attr.SlogError(err))
	}
	withEndpoints, endpointsErr := repo.New(db).ListMCPServerIDsWithEndpoints(ctx, repo.ListMCPServerIDsWithEndpointsParams{
		OrganizationID: organizationID,
		ProjectIds:     projectIDs,
		McpServerIds:   serverIDs,
	})
	if endpointsErr != nil {
		logger.WarnContext(ctx, "list mcp servers with endpoints", attr.SlogError(endpointsErr))
	}
	legacySlugs, legacyErr := repo.New(db).ListLegacyPlatformAliasSlugs(ctx, repo.ListLegacyPlatformAliasSlugsParams{
		OrganizationID: organizationID,
		ProjectIds:     projectIDs,
		McpServerIds:   serverIDs,
	})
	if legacyErr != nil {
		logger.WarnContext(ctx, "list legacy platform alias slugs", attr.SlogError(legacyErr))
	}
	legacySlug := make(map[uuid.UUID]string, len(legacySlugs))
	for _, row := range legacySlugs {
		legacySlug[row.McpServerID] = row.Slug
	}
	hasEndpoint := make(map[uuid.UUID]struct{}, len(withEndpoints))
	for _, id := range withEndpoints {
		hasEndpoint[id] = struct{}{}
	}

	for i, server := range servers {
		if server.ToolsetID.Valid {
			views[i].Toolset = summaries[server.ToolsetID.UUID]
		}
		if slug, ok := legacySlug[server.ID]; ok {
			views[i].LegacyPlatformSlug = new(slug)
		}
		if endpointsErr == nil {
			_, has := hasEndpoint[server.ID]
			views[i].HasEndpoint = new(has)
		}
		primary, ok := primaries[server.ID]
		if !ok {
			continue
		}
		views[i].PrimaryEndpointID = new(primary.ID.String())
		if primary.URL != "" {
			views[i].PrimaryEndpointURL = new(primary.URL)
		}
		if primary.PlatformSlug != "" {
			views[i].PlatformEndpointSlug = new(primary.PlatformSlug)
		}
	}
	return err == nil
}

// buildMcpServerView builds one server's enriched view.
func (s *Service) buildMcpServerView(ctx context.Context, organizationID string, server repo.McpServer) (*types.McpServer, bool) {
	view := mv.BuildMcpServerView(server)
	resolved := EnrichViews(ctx, s.logger, s.db, s.serverURL, organizationID, []repo.McpServer{server}, []*types.McpServer{view})
	return view, resolved
}
