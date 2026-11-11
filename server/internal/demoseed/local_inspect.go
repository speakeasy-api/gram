package demoseed

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/speakeasy-api/gram/server/internal/demoseed/mocktasks"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// localTasksKey names the local mock task tracker's rows (s.det inputs and
// per-tool block ids).
const localTasksKey = "local-tasks"

// localUnreachableURL is an upstream nothing listens on (the discard port),
// so the Inspect tab shows its connection error state.
const localUnreachableURL = "http://127.0.0.1:9/mcp"

// localTasksProfile is the seeded call shape of each mock tool over the last
// seven days: calls, error rate, median seconds. list_labels has no calls.
var localTasksProfile = map[string]struct {
	calls     int
	errorRate float64
	p50       float64
}{
	"search_tasks":           {160, 0.004, 0.22},
	"get_task":               {120, 0.003, 0.12},
	"list_projects":          {60, 0, 0.09},
	"list_labels":            {0, 0, 0},
	"create_task":            {70, 0.06, 0.35},
	"update_task":            {45, 0.005, 0.3},
	"delete_task":            {8, 0, 0.28},
	"archive_project":        {3, 0, 0.4},
	"sync_external_calendar": {40, 0.18, 0.9},
	"generate_quarterly_workspace_activity_report_for_all_teams_and_projects": {25, 0.008, 2.6},
	mocktasks.UnlistedTool: {5, 0, 0.5},
}

// localTasksTools is the mock's catalog as stored tool metadata, plus the
// tool the mock no longer lists.
func localTasksTools() []inspectTool {
	tools := make([]inspectTool, 0, len(mocktasks.Tools)+1)
	for _, t := range mocktasks.Tools {
		p := localTasksProfile[t.Name]
		tools = append(tools, inspectTool{t.Name, t.Title, t.ReadOnly, t.Destructive, t.Idempotent, t.OpenWorld, p.calls, p.errorRate, p.p50})
	}
	p := localTasksProfile[mocktasks.UnlistedTool]
	return append(tools, inspectTool{mocktasks.UnlistedTool, "Bulk reassign tasks", false, false, true, false, p.calls, p.errorRate, p.p50})
}

// localInspectServer is one local-only remote server.
type localInspectServer struct {
	key, name, url, visibility string
}

// runLocalInspectFixtures adds the remote servers the Inspect tab needs to
// exercise locally: the mock task tracker (works offline), an unreachable
// upstream, and a disabled copy of the mock; the mock's tool metadata; an
// Admin block on delete_task; Support Desk limited to its read-only tools;
// and the mock as a member of the Direct mode gateway. Every row is in the
// seeded project or a custom role's grants, so the next seed run clears it.
// An empty mockURL leaves the mock-backed servers out.
func runLocalInspectFixtures(ctx context.Context, logger *slog.Logger, q *repo.Queries, now time.Time, mockURL string) (*seeder, error) {
	s := &seeder{spec: LocalSpec(), enc: nil, q: q, logger: logger, now: now, signalSequences: nil}
	proj := s.projectID()

	servers := []localInspectServer{{key: "local-unreachable", name: "Unreachable server (local)", url: localUnreachableURL, visibility: "private"}}
	if mockURL != "" {
		servers = append(servers,
			localInspectServer{key: localTasksKey, name: "Acme Tasks (local mock)", url: mockURL, visibility: "private"},
			localInspectServer{key: "local-tasks-disabled", name: "Acme Tasks, disabled (local)", url: mockURL, visibility: "disabled"},
		)
	} else {
		logger.WarnContext(ctx, "GRAM_MOCK_MCP_URL is unset, so the local mock MCP server is not registered")
	}
	for _, srv := range servers {
		if err := q.LocalUpsertInspectRemoteServer(ctx, repo.LocalUpsertInspectRemoteServerParams{
			ID: s.det("remotemcp-" + srv.key), ProjectID: proj, Name: srv.name, Slug: srv.key, Url: srv.url,
		}); err != nil {
			return nil, fmt.Errorf("upsert %s remote server: %w", srv.key, err)
		}
		if err := q.LocalUpsertInspectMCPServer(ctx, repo.LocalUpsertInspectMCPServerParams{
			ID: s.det("mcpserver-" + srv.key), ProjectID: proj, Name: srv.name, Slug: srv.key,
			RemoteMcpServerID: s.det("remotemcp-" + srv.key), UserSessionIssuerID: s.det("issuer-workforce"),
			Visibility: srv.visibility,
		}); err != nil {
			return nil, fmt.Errorf("upsert %s mcp server: %w", srv.key, err)
		}
		if err := q.LocalUpsertInspectEndpoint(ctx, repo.LocalUpsertInspectEndpointParams{
			ID: s.det("endpoint-" + srv.key), ProjectID: proj, McpServerID: s.det("mcpserver-" + srv.key),
			Slug: s.spec.OrgSlug + "-" + srv.key,
		}); err != nil {
			return nil, fmt.Errorf("upsert %s endpoint: %w", srv.key, err)
		}
	}
	if mockURL == "" {
		return s, nil
	}

	tasks := s.det("mcpserver-" + localTasksKey)
	if err := s.upsertToolMetadata(ctx, tasks, localTasksTools()); err != nil {
		return nil, fmt.Errorf("local tasks tool metadata: %w", err)
	}
	n, err := q.ServersInsertToolBlock(ctx, repo.ServersInsertToolBlockParams{
		ID: s.toolBlockID("admin", localTasksKey, "delete_task"), OrgID: s.orgID(),
		McpServerID: tasks, Tool: "delete_task", RoleSlug: "admin",
	})
	if err != nil {
		return nil, fmt.Errorf("block delete_task for admins: %w", err)
	}
	if n != 1 {
		logger.WarnContext(ctx, "no Admin role to block delete_task for; the Inspect tab shows no blocked tool")
	}
	if _, err := q.LocalGrantRoleReadOnlyConnect(ctx, repo.LocalGrantRoleReadOnlyConnectParams{
		OrganizationID: s.orgID(), McpServerID: tasks, RoleSlug: "support-desk",
	}); err != nil {
		return nil, fmt.Errorf("grant support desk read-only tasks: %w", err)
	}
	if err := q.ServersInsertGatewayMember(ctx, repo.ServersInsertGatewayMemberParams{
		ID: s.det("metamember-direct-" + localTasksKey), ProjectID: proj, MetaID: s.directGatewayID(),
		McpServerID: tasks, SortOrder: 3,
	}); err != nil {
		return nil, fmt.Errorf("add local tasks to the direct gateway: %w", err)
	}
	return s, nil
}

// insertLocalInspectTelemetry seeds the mock's direct calls and its calls
// through the Direct mode gateway. The next seed run's ClickHouse purge,
// scoped to the local project, removes them. The rows also feed aggregated
// summary tables, so they cannot be deleted on their own: when this seed
// generation already holds them, the insert is skipped instead, which keeps
// repeated RunLocalFixtures calls from duplicating them.
func insertLocalInspectTelemetry(ctx context.Context, s *seeder, ch driver.Conn) error {
	tasks, remote := s.det("mcpserver-"+localTasksKey), s.det("remotemcp-"+localTasksKey)

	var existing uint64
	if err := ch.QueryRow(ctx,
		"SELECT count() FROM telemetry_logs WHERE gram_project_id = toUUID(?) AND startsWith(gram_urn, ?)",
		s.projectID().String(), "tools:externalmcp:"+remote.String()+":",
	).Scan(&existing); err != nil {
		return fmt.Errorf("count existing local mock telemetry: %w", err)
	}
	if existing > 0 {
		return nil
	}

	tools := localTasksTools()
	rows := s.directRemoteCalls(localTasksKey, tasks, remote, "", tools)
	dispatched := make([]inspectTool, 0, len(tools))
	for _, t := range tools {
		t.calls /= 4
		dispatched = append(dispatched, t)
	}
	rows = append(rows, s.directRemoteCalls("gwdirect-"+localTasksKey, tasks, remote, s.directGatewayID().String(), dispatched)...)
	return s.chfInsertTelemetry(ctx, ch, "local mock remote server telemetry", rows)
}
