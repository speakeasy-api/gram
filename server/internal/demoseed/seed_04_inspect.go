package demoseed

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// inspectTool is one tool of a remote server as the Inspect tab shows it:
// the annotations stored in mcp_server_tool_metadata, and the shape of its
// seeded direct calls over the last seven days.
type inspectTool struct {
	name, title                                  string
	readOnly, destructive, idempotent, openWorld bool
	// calls is the number of direct calls in the last seven days; 0 is a
	// tool nobody called.
	calls int
	// errorRate is the share of those calls that failed.
	errorRate float64
	// p50 is the median call duration in seconds.
	p50 float64
}

// inspectRemoteTools are the tools of the shared remote servers. The names
// include the ones the gateway dispatch rows already use (chfGatewayTools,
// chfGatewayGoneCalls), so gateway and direct calls land on the same rows.
// Per server: most tools healthy (<1% errors), one at ~6%, one at ~18% or a
// slow one, and one destructive tool nobody called this week.
var inspectRemoteTools = map[string][]inspectTool{
	"linear": {
		{"search_issues", "Search issues", true, false, true, true, 180, 0.004, 0.38},
		{"list_issues", "List issues", true, false, true, true, 140, 0.005, 0.42},
		{"get_issue", "Get issue", true, false, true, true, 120, 0.003, 0.21},
		{"list_teams", "List teams", true, false, true, true, 40, 0, 0.16},
		{"create_issue", "Create issue", false, false, false, true, 90, 0.06, 0.74},
		{"update_issue", "Update issue", false, false, true, true, 60, 0.008, 0.55},
		{"create_comment", "Create comment", false, false, false, true, 50, 0.004, 0.47},
		{"delete_issue", "Delete issue", false, true, true, true, 6, 0, 0.51},
		{"archive_project", "Archive project", false, true, true, true, 0, 0, 0},
	},
	"slack": {
		{"list_channels", "List channels", true, false, true, true, 130, 0.004, 0.24},
		{"read_channel_history", "Read channel history", true, false, true, true, 110, 0.006, 0.61},
		{"search_messages", "Search messages", true, false, true, true, 95, 0.005, 0.83},
		{"get_user_profile", "Get user profile", true, false, true, true, 45, 0, 0.18},
		{"send_message", "Send message", false, false, false, true, 120, 0.18, 0.66},
		{"add_reaction", "Add reaction", false, false, true, true, 35, 0.005, 0.2},
		{"delete_message", "Delete message", false, true, true, true, 4, 0, 0.35},
		{"archive_channel", "Archive channel", false, true, true, true, 0, 0, 0},
	},
	"github": {
		{"search_code", "Search code", true, false, true, true, 110, 0.007, 2.4},
		{"list_pull_requests", "List pull requests", true, false, true, true, 120, 0.004, 0.45},
		{"get_issue", "Get issue", true, false, true, true, 70, 0.003, 0.31},
		{"get_file_contents", "Get file contents", true, false, true, true, 85, 0.005, 0.36},
		{"create_issue", "Create issue", false, false, false, true, 30, 0.06, 0.9},
		{"create_pull_request", "Create pull request", false, false, false, true, 25, 0.008, 1.3},
		{"merge_pull_request", "Merge pull request", false, false, true, true, 14, 0, 1.1},
		{"delete_branch", "Delete branch", false, true, true, true, 9, 0, 0.42},
		{"delete_repository", "Delete repository", false, true, true, true, 0, 0, 0},
	},
}

// inspectToolBlocks are per-tool exclusions (mcp:blocked_connect with a tool
// selector): each role still connects to the server and its other tools.
// The Admin block is what gives a signed-in admin a blocked tool to see.
var inspectToolBlocks = []struct{ role, server, tool string }{
	{"engineer", "linear", "delete_issue"},
	{"contractors", "slack", "delete_message"},
	{"contractors", "github", "delete_branch"},
	{"admin", "linear", "delete_issue"},
}

// toolBlockID is the grant id of one per-tool block.
func (s *seeder) toolBlockID(role, server, tool string) uuid.UUID {
	return s.det("tool-block-" + role + "-" + server + "-" + tool)
}

// globalToolBlockIDs are the per-tool blocks held by global roles, which
// outlive the custom roles' grant cleanup and so are deleted by id.
func (s *seeder) globalToolBlockIDs() []uuid.UUID {
	return []uuid.UUID{
		s.toolBlockID("admin", "linear", "delete_issue"),
		s.toolBlockID("admin", localTasksKey, "delete_task"),
	}
}

// seedInspect: stored tool annotations, per-tool role blocks, and a Direct
// mode gateway, for the remote server Inspect tab and the gateway pages.
func (s *seeder) seedInspect(ctx context.Context) error {
	org := s.orgID()
	proj := s.projectID()
	for _, server := range []string{"linear", "slack", "github"} {
		if err := s.upsertToolMetadata(ctx, s.det("mcpserver-"+server), inspectRemoteTools[server]); err != nil {
			return fmt.Errorf("insert demo %s tool metadata: %w", server, err)
		}
	}
	for _, b := range inspectToolBlocks {
		n, err := s.q.ServersInsertToolBlock(ctx, repo.ServersInsertToolBlockParams{
			ID: s.toolBlockID(b.role, b.server, b.tool), OrgID: org,
			McpServerID: s.det("mcpserver-" + b.server), Tool: b.tool, RoleSlug: b.role,
		})
		if err != nil {
			return fmt.Errorf("insert demo %s block on %s %s: %w", b.role, b.server, b.tool, err)
		}
		// Custom roles always exist by now; the global Admin role may not on
		// a fresh local stack (RunLocalFixtures seeds it).
		if n != 1 && b.role != "admin" {
			return fmt.Errorf("demo seed: %s block on %s %s inserted %d rows", b.role, b.server, b.tool, n)
		}
	}

	if err := s.q.ServersInsertDirectGatewayIssuer(ctx, repo.ServersInsertDirectGatewayIssuerParams{
		ID: s.det("issuer-direct-gateway"), ProjectID: proj, OrgID: org,
	}); err != nil {
		return fmt.Errorf("insert demo direct gateway issuer: %w", err)
	}
	if err := s.q.ServersInsertDirectGateway(ctx, repo.ServersInsertDirectGatewayParams{
		ID: s.directGatewayID(), OrgID: org, ProjectID: proj, UserSessionIssuerID: s.det("issuer-direct-gateway"),
	}); err != nil {
		return fmt.Errorf("insert demo direct gateway: %w", err)
	}
	for i, member := range []string{"linear", "github", "deepwiki"} {
		if err := s.q.ServersInsertGatewayMember(ctx, repo.ServersInsertGatewayMemberParams{
			ID: s.det("metamember-direct-" + member), ProjectID: proj, MetaID: s.directGatewayID(),
			McpServerID: s.det("mcpserver-" + member), SortOrder: int32(i),
		}); err != nil {
			return fmt.Errorf("insert demo direct gateway member %s: %w", member, err)
		}
	}
	if err := s.q.ServersInsertCodeGatewayEndpoint(ctx, repo.ServersInsertCodeGatewayEndpointParams{
		ID: s.det("endpoint-direct-gateway"), ProjectID: proj, MetaID: s.directGatewayID(),
		Slug: s.spec.OrgSlug + "-direct-gateway",
	}); err != nil {
		return fmt.Errorf("insert demo direct gateway endpoint: %w", err)
	}
	return nil
}

// directGatewayID is the Direct mode gateway, joined by its ClickHouse rows.
func (s *seeder) directGatewayID() uuid.UUID { return s.det("metamcp-direct") }

// upsertToolMetadata stores one server's tool annotations.
func (s *seeder) upsertToolMetadata(ctx context.Context, server uuid.UUID, tools []inspectTool) error {
	p := repo.ServersUpsertToolMetadataParams{
		ProjectID: s.projectID(), McpServerID: server,
		ToolNames: nil, Titles: nil, ReadOnlyHints: nil, DestructiveHints: nil, IdempotentHints: nil, OpenWorldHints: nil,
	}
	for _, t := range tools {
		p.ToolNames = append(p.ToolNames, t.name)
		p.Titles = append(p.Titles, t.title)
		p.ReadOnlyHints = append(p.ReadOnlyHints, t.readOnly)
		p.DestructiveHints = append(p.DestructiveHints, t.destructive)
		p.IdempotentHints = append(p.IdempotentHints, t.idempotent)
		p.OpenWorldHints = append(p.OpenWorldHints, t.openWorld)
	}
	n, err := s.q.ServersUpsertToolMetadata(ctx, p)
	if err != nil {
		return fmt.Errorf("upsert tool metadata: %w", err)
	}
	if n != int64(len(tools)) {
		return fmt.Errorf("upsert tool metadata: wrote %d of %d rows", n, len(tools))
	}
	return nil
}

// inspectMetadataCount is the number of stored tool annotations the shared
// seed writes, for the postflight.
func inspectMetadataCount() int64 {
	var n int64
	for _, tools := range inspectRemoteTools {
		n += int64(len(tools))
	}
	return n
}

// directCallCount is the number of direct calls a tool list seeds.
func directCallCount(tools []inspectTool) int {
	n := 0
	for _, t := range tools {
		n += t.calls
	}
	return n
}

// directCallWindowMinutes is the seven days direct calls are spread over.
const directCallWindowMinutes = 7 * 24 * 60

// directRemoteCalls builds the calls made straight to a remote server's own
// endpoint over the last seven days: ordinary tool_call rows for the server
// (the "tools:externalmcp:<remote id>:" urn and tool call source the proxy
// stamps), so the per-tool breakdown filtered by the server's id has a row
// per called tool. A non-empty gateway stamps the rows as dispatched through
// it instead. key salts the draws and trace ids.
func (s *seeder) directRemoteCalls(key string, server, remote uuid.UUID, gateway string, tools []inspectTool) []chfTelemetryRow {
	rows := make([]chfTelemetryRow, 0, directCallCount(tools))
	for _, t := range tools {
		failures := int(math.Round(float64(t.calls) * t.errorRate))
		for k := range t.calls {
			salt := key + "-" + t.name
			urn := "tools:externalmcp:" + remote.String() + ":" + t.name
			status := 200
			// Spread the failures evenly through the week.
			if failures > 0 && k%(t.calls/failures) == 0 && k/(t.calls/failures) < failures {
				status = []int{500, 502, 504}[hash64(salt+"st", k)%3]
			}
			// Median p50; a long tail of one call in twenty at 2-4x.
			factor := 0.55 + float64(hash64(salt+"lat", k)%900)/1000
			if hash64(salt+"tail", k)%20 == 0 {
				factor = 2 + float64(hash64(salt+"tl", k)%200)/100
			}
			email := s.email(demoUsers[hash64(salt+"u", k)%uint64(len(demoUsers))].handle)
			var sb strings.Builder
			sb.WriteString(`{"gram.tool.urn":"` + urn + `"`)
			sb.WriteString(`,"gram.tool.name":"` + t.name + `"`)
			sb.WriteString(`,"gram.event.source":"tool_call"`)
			sb.WriteString(`,"gram.mcp_server.id":"` + server.String() + `"`)
			sb.WriteString(`,"gram.remote_mcp_server.id":"` + remote.String() + `"`)
			sb.WriteString(`,"gram.tool_call.source":"` + remote.String() + `"`)
			if gateway != "" {
				sb.WriteString(`,"gram.meta_mcp_server.id":"` + gateway + `"`)
			}
			sb.WriteString(`,"http.response.status_code":` + strconv.Itoa(status))
			sb.WriteString(`,"http.server.request.duration":` + chfRound3(t.p50*factor))
			sb.WriteString(`,"gram.project.id":"` + s.projectID().String() + `"`)
			sb.WriteString(`,"user.email":"` + email + `"`)
			sb.WriteString(`,"gram.external_user.id":"` + email + `"`)
			sb.WriteString(chfEmailClient(email) + `}`)
			// Never the last half hour, so nothing lands in the future.
			ago := time.Duration(30+hash64(salt+"ts", k)%(directCallWindowMinutes-30)) * time.Minute
			rows = append(rows, chfTelemetryRow{
				ts:      s.now.UTC().Add(-ago),
				body:    "Tool call: " + t.name,
				traceID: md5Hex(s.name("direct-" + salt + "-" + strconv.Itoa(k))),
				attrs:   sb.String(),
				urn:     urn,
				service: "gram-server",
				chatID:  nil,
			})
		}
	}
	return rows
}
