package platformmcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/servernames"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// serverIdentity is every identity one configured MCP's telemetry is recorded
// under. A gateway row carries the configured id for any proxied model and the
// toolset slug for a hosted one; a hook row carries the URL the client called
// when the hook resolved the server, and otherwise only the name the agent
// used for it. A read that narrows by all of them attributes every lane's
// calls to the one configured server, and a read that narrows by none of them
// would read the whole project under that server's name.
type serverIdentity struct {
	// mcpServerID is the configured id. It is always present, so a read is
	// never left unscoped.
	mcpServerID string

	// toolsetSlug is the hosted toolset the server fronts. It is empty for a
	// remote, tunneled, or unproxied server, and for a toolset that more than
	// one configured server wraps, where the slug cannot single one out.
	//
	// Emptiness matters: the summary reads treat an empty slug as "no filter",
	// so a caller that passes it through unchecked gets the whole project's
	// numbers back under one MCP's name. Every use of this value must handle
	// the empty case rather than forwarding it.
	toolsetSlug string

	// urlSuffixes is how the server appears in the URL a hook-observed client
	// called, or empty when the server has no slug.
	urlSuffixes []string

	// toolSources is every name an agent may have reported the server under
	// that identifies it alone, spelled as the agent reports it.
	toolSources []string
}

// outcomeParams narrows a call-level read to this server within one project
// and window.
func (id serverIdentity) outcomeParams(projectID string, start, end int64) telemetryrepo.GetMCPOutcomeBreakdownParams {
	return telemetryrepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs:       []string{projectID},
		ToolsetSlugs:         nonEmpty(id.toolsetSlug),
		MCPServerURLSuffixes: id.urlSuffixes,
		MCPServerIDs:         []string{id.mcpServerID},
		ToolSources:          id.toolSources,
		CanonicalIdentityOrg: "",
		TimeStart:            start,
		TimeEnd:              end,
		Limit:                0,
	}
}

// activeCountsParams narrows the active-user count to this server within one
// project and window, under every identity it is recorded by: a hosted call
// carries the toolset slug, a proxied call the configured id, and a
// hook-observed call the URL or name the agent used. The count read matches
// any of them, so a server whose calls arrive only through hooks still counts
// its users.
func (id serverIdentity) activeCountsParams(projectID string, start, end int64) telemetryrepo.GetActiveCountsParams {
	return telemetryrepo.GetActiveCountsParams{
		GramProjectID:        projectID,
		TimeStart:            start,
		TimeEnd:              end,
		ExternalUserID:       "",
		APIKeyID:             "",
		ToolsetSlug:          id.toolsetSlug,
		MCPServerID:          id.mcpServerID,
		MCPServerURLSuffixes: id.urlSuffixes,
		ToolSources:          id.toolSources,
		SessionMode:          false,
	}
}

// serverIdentity resolves one configured MCP to every identity its telemetry
// is recorded under, from the one listing of the project's configured servers
// that also feeds name resolution. The listing is scoped to the organization's
// own project, so a caller cannot resolve an MCP it cannot already see.
func (s *DiagnosticsService) serverIdentity(ctx context.Context, organizationID, projectID, mcpID string) (serverIdentity, error) {
	parsedMCP, err := uuid.Parse(mcpID)
	if err != nil {
		return serverIdentity{}, fmt.Errorf("parse mcp id: %w", err)
	}
	id := parsedMCP.String()
	servers, err := s.listConfiguredServers(ctx, organizationID, projectID)
	if err != nil {
		return serverIdentity{}, err
	}
	var target *servernames.ConfiguredServer
	for i := range servers {
		if servers[i].ID == id {
			target = &servers[i]
			break
		}
	}
	if target == nil {
		return serverIdentity{}, ErrDiagnosticsTargetNotFound
	}
	toolsetSlug := target.ToolsetSlug
	if toolsetSlug != "" {
		// Direct telemetry identifies only the toolset. When several configured
		// MCP wrappers share it, those rows cannot be attributed to one wrapper.
		wrappers := 0
		for _, server := range servers {
			if server.ToolsetSlug == toolsetSlug {
				wrappers++
			}
		}
		if wrappers > 1 {
			toolsetSlug = ""
		}
	}
	return serverIdentity{
		mcpServerID: id,
		toolsetSlug: toolsetSlug,
		urlSuffixes: mcpURLSuffixes(target.Slug),
		toolSources: servernames.NewResolver(servers).ReportedNames(id),
	}, nil
}

// serverNameResolver indexes every configured server in the project by the
// names an agent may report it under.
func (s *DiagnosticsService) serverNameResolver(ctx context.Context, organizationID, projectID string) (*servernames.Resolver, error) {
	servers, err := s.listConfiguredServers(ctx, organizationID, projectID)
	if err != nil {
		return nil, err
	}
	return servernames.NewResolver(servers), nil
}

// listConfiguredServers lists the project's live configured servers with every
// name each can be reported under.
func (s *DiagnosticsService) listConfiguredServers(ctx context.Context, organizationID, projectID string) ([]servernames.ConfiguredServer, error) {
	parsedProject, err := uuid.Parse(projectID)
	if err != nil {
		return nil, fmt.Errorf("parse project id: %w", err)
	}
	rows, err := platformrepo.New(s.db).ListPlatformMCPServerIdentities(ctx, platformrepo.ListPlatformMCPServerIdentitiesParams{
		OrganizationID: organizationID,
		ProjectID:      parsedProject,
	})
	if err != nil {
		return nil, fmt.Errorf("list mcp server identities: %w", err)
	}
	return configuredServers(rows), nil
}

// configuredServers folds the one-row-per-membership listing into one entry
// per configured server, preserving first-seen order.
func configuredServers(rows []platformrepo.ListPlatformMCPServerIdentitiesRow) []servernames.ConfiguredServer {
	index := map[string]int{}
	servers := make([]servernames.ConfiguredServer, 0, len(rows))
	for _, row := range rows {
		id := row.McpServerID.String()
		position, seen := index[id]
		if !seen {
			position = len(servers)
			index[id] = position
			servers = append(servers, servernames.ConfiguredServer{
				ID:          id,
				Name:        row.McpName,
				Slug:        row.McpSlug,
				ToolsetSlug: row.ToolsetSlug,
				Plugins:     nil,
			})
		}
		// A server with no membership is listed once with empty plugin columns.
		if row.PluginSlug == "" {
			continue
		}
		servers[position].Plugins = append(servers[position].Plugins, servernames.PluginMembership{
			PluginSlug:  row.PluginSlug,
			DisplayName: row.PluginDisplayName,
		})
	}
	return servers
}

// attributeTopServers folds hook-reported server names onto configured
// servers. Rows an agent reported under different names for one configured
// server — a plugin-routed prefix, a bare slug, the display name, the id — sum
// into one entry named by the configured server and carrying its id. A name no
// configured server is known by stays as the agent reported it, with no id,
// so a caller can still see what was used without being told it is
// configured. The result is ordered by calls, then name, and capped at limit.
func attributeTopServers(rows []telemetryrepo.TopServer, resolver *servernames.Resolver, limit int) []ProjectOverviewServer {
	byKey := map[string]*ProjectOverviewServer{}
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		key := "reported:" + row.ServerName
		entry := ProjectOverviewServer{Name: row.ServerName, MCPID: "", ToolCalls: 0}
		if id, ok := resolver.Resolve(row.ServerName); ok {
			key = "mcp:" + id
			entry = ProjectOverviewServer{Name: resolver.Name(id), MCPID: id, ToolCalls: 0}
		}
		folded, seen := byKey[key]
		if !seen {
			folded = &entry
			byKey[key] = folded
			order = append(order, key)
		}
		folded.ToolCalls += boundedCount(row.ToolCallCount)
	}
	servers := make([]ProjectOverviewServer, 0, len(order))
	for _, key := range order {
		servers = append(servers, *byKey[key])
	}
	sort.SliceStable(servers, func(i, j int) bool {
		if servers[i].ToolCalls != servers[j].ToolCalls {
			return servers[i].ToolCalls > servers[j].ToolCalls
		}
		return servers[i].Name < servers[j].Name
	})
	if len(servers) > limit {
		servers = servers[:limit]
	}
	return servers
}

// metricTotals is what query_mcp_metrics reports once the call-level tally and
// the gateway summary of one window are reconciled.
type metricTotals struct {
	toolCalls       int64
	failedToolCalls int64
	avgLatencyMs    float64
	observed        bool
}

// reconcileMetrics combines the two reads of one server's window. The
// call-level tally leads because it classifies outcomes. When it saw nothing
// but the gateway summary counted calls, the summary's counts are reported
// instead: a zero beside the nonzero latency the same summary produced would
// contradict itself. The window counts as observed when either read holds
// anything, so no_observations is never asserted alongside a nonzero metric.
func reconcileMetrics(totals outcomeTotals, summary *telemetryrepo.OverviewSummary) metricTotals {
	metrics := metricTotals{
		toolCalls:       totals.Total,
		failedToolCalls: totals.failures(),
		avgLatencyMs:    0,
		observed:        false,
	}
	if summary != nil {
		metrics.avgLatencyMs = summary.AvgLatencyMs
		if metrics.toolCalls == 0 && summary.TotalToolCalls > 0 {
			metrics.toolCalls = boundedCount(summary.TotalToolCalls)
			metrics.failedToolCalls = boundedCount(summary.FailedToolCalls)
		}
	}
	metrics.observed = metrics.toolCalls > 0 || metrics.avgLatencyMs > 0
	return metrics
}
