//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	telemetrysvc "github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	defaultRecentToolCallLimit = 10
	maxRecentToolCallLimit     = 50
)

// RecentToolCallReader is the bounded Tool Logs summary read used by Platform
// MCP. It intentionally cannot read raw log bodies or attributes.
type RecentToolCallReader interface {
	ListToolUsageTraces(ctx context.Context, arg telemetryrepo.ListToolUsageTracesParams) ([]telemetryrepo.ToolUsageTraceSummary, error)
}

// RecentToolCallReadService owns the summary reader and trusted dashboard URL.
type RecentToolCallReadService struct {
	telemetry    RecentToolCallReader
	dashboardURL *url.URL
	now          func() time.Time
}

// WithRecentToolCalls enables recent project-scoped Tool Logs summaries.
func (r *PostgresReader) WithRecentToolCalls(telemetry RecentToolCallReader, dashboardURL *url.URL) *PostgresReader {
	if r != nil && r.db != nil && telemetry != nil && validDashboardURL(dashboardURL) {
		copyURL := *dashboardURL
		r.recentToolCalls = &RecentToolCallReadService{telemetry: telemetry, dashboardURL: &copyURL, now: time.Now}
	}
	return r
}

type ListRecentToolCallsInput struct {
	ProjectID   string `json:"project_id,omitempty" jsonschema:"project ID to inspect; supply exactly one project selector"`
	ProjectSlug string `json:"project_slug,omitempty" jsonschema:"project slug to inspect; supply exactly one project selector"`
	Window      string `json:"window,omitempty" jsonschema:"observation window: 1h (default) or 24h"`
	Outcome     string `json:"outcome,omitempty" jsonschema:"optional outcome filter: success, error, blocked, or pending"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum calls to return; defaults to 10 and is capped at 50"`
}

type RecentToolCall struct {
	OccurredAt string `json:"occurred_at"`
	ToolName   string `json:"tool_name,omitempty"`
	TargetType string `json:"target_type"`
	TargetKind string `json:"target_kind"`
	Target     string `json:"target,omitempty"`
	Outcome    string `json:"outcome"`
	Client     string `json:"client,omitempty"`
}

type ListRecentToolCallsOutput struct {
	ProjectID   string           `json:"project_id"`
	ProjectName string           `json:"project_name"`
	ProjectSlug string           `json:"project_slug"`
	Window      ResolvedWindow   `json:"window"`
	Calls       []RecentToolCall `json:"calls"`
	More        bool             `json:"more"`
	ToolLogsURL string           `json:"tool_logs_url"`
}

func (r *PostgresReader) ListRecentToolCalls(ctx context.Context, principal Principal, input ListRecentToolCallsInput) (ListRecentToolCallsOutput, error) {
	if r == nil || r.reader == nil || r.recentToolCalls == nil || r.recentToolCalls.telemetry == nil || r.recentToolCalls.now == nil {
		return ListRecentToolCallsOutput{}, ErrUnavailable
	}
	if (input.ProjectID == "") == (input.ProjectSlug == "") {
		return ListRecentToolCallsOutput{}, fmt.Errorf("exactly one of project_id or project_slug is required")
	}
	outcome := strings.ToLower(strings.TrimSpace(input.Outcome))
	if outcome != "" && !validRecentToolCallOutcome(outcome) {
		return ListRecentToolCallsOutput{}, fmt.Errorf("outcome must be one of success, error, blocked, pending")
	}

	project, err := r.resolveInventoryProject(ctx, principal.OrganizationID, FindMCPInput{ProjectID: input.ProjectID, ProjectSlug: input.ProjectSlug})
	if err != nil {
		return ListRecentToolCallsOutput{}, err
	}
	now := r.recentToolCalls.now().UTC()
	window, err := resolveWindow(input.Window, now, windowSpec{Fallback: DiagnosticWindowLastHour, Max: DiagnosticWindowLastDay})
	if err != nil {
		return ListRecentToolCallsOutput{}, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultRecentToolCallLimit
	}
	limit = min(limit, maxRecentToolCallLimit)
	statuses := []string(nil)
	if outcome != "" {
		statuses = []string{outcome}
	}

	hostedMCPMatchers, mcpServerMatchers, err := telemetrysvc.LoadToolUsageMatchers(ctx, r.db, project.ID)
	if err != nil {
		return ListRecentToolCallsOutput{}, fmt.Errorf("load recent tool call matchers: %w", err)
	}

	rows, err := r.recentToolCalls.telemetry.ListToolUsageTraces(ctx, telemetryrepo.ListToolUsageTracesParams{
		GramProjectID:      project.ID.String(),
		TimeStart:          window.start.UnixNano(),
		TimeEnd:            window.end.UnixNano(),
		HostedMCPMatchers:  hostedMCPMatchers,
		MCPServerMatchers:  mcpServerMatchers,
		TargetTypes:        nil,
		HostedToolsetSlugs: nil,
		ShadowServerNames:  nil,
		UserFilters:        nil,
		HookSources:        nil,
		AccountType:        "",
		Statuses:           statuses,
		Query:              "",
		Filters:            nil,
		SortOrder:          "desc",
		CursorTimeUnixNano: 0,
		CursorID:           "",
		Limit:              limit + 1,
	})
	if err != nil {
		return ListRecentToolCallsOutput{}, fmt.Errorf("list recent tool calls: %w", err)
	}
	rows, more := boundedRows(rows, limit)
	calls := make([]RecentToolCall, 0, len(rows))
	for _, row := range rows {
		calls = append(calls, RecentToolCall{
			OccurredAt: time.Unix(0, row.StartTimeUnixNano).UTC().Format(time.RFC3339Nano),
			ToolName:   row.ToolName,
			TargetType: row.TargetType,
			TargetKind: row.TargetKind,
			Target:     recentToolCallTarget(row),
			Outcome:    recentToolCallOutcome(row),
			Client:     dereferenceString(row.HookSource),
		})
	}
	organization, err := organizationsrepo.New(r.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	if err != nil {
		return ListRecentToolCallsOutput{}, fmt.Errorf("resolve recent tool calls organization: %w", err)
	}
	return ListRecentToolCallsOutput{
		ProjectID:   project.ID.String(),
		ProjectName: project.Name,
		ProjectSlug: project.Slug,
		Window:      window,
		Calls:       calls,
		More:        more,
		ToolLogsURL: r.recentToolCalls.dashboardURL.JoinPath(organization.Slug, "projects", project.Slug, "logs").String(),
	}, nil
}

func validRecentToolCallOutcome(outcome string) bool {
	switch outcome {
	case "success", "error", "blocked", "pending":
		return true
	default:
		return false
	}
}

func recentToolCallOutcome(row telemetryrepo.ToolUsageTraceSummary) string {
	if row.HookStatus != nil {
		if *row.HookStatus == "failure" {
			return "error"
		}
		return *row.HookStatus
	}
	if row.HTTPStatusCode == nil {
		return "unknown"
	}
	if *row.HTTPStatusCode >= 400 {
		return "error"
	}
	if *row.HTTPStatusCode >= 200 {
		return "success"
	}
	return "unknown"
}

func recentToolCallTarget(row telemetryrepo.ToolUsageTraceSummary) string {
	if row.TargetType == telemetryrepo.ToolUsageTargetTypeShadowMCP {
		return ""
	}
	return row.TargetLabel
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func registerRecentToolCallTools(reg *Registrar, reader *PostgresReader) {
	addTool(reg, &mcp.Tool{
		Name:        "list_recent_tool_calls",
		Title:       "List Recent Tool Calls",
		Description: "List the newest Tool Logs summaries for one project, defaulting to 10 calls from the last hour. Each call is reduced to when it happened, the tool and target, how it ended, and the calling app when known. Constraints: this uses the bounded Tool Logs summary path and never returns arguments, results, bodies, headers, URLs, attributes, or user identities. The returned dashboard link opens the full Tool Logs page.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListRecentToolCallsInput) (*mcp.CallToolResult, ListRecentToolCallsOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, ListRecentToolCallsOutput{}, err
		}
		output, err := reader.ListRecentToolCalls(ctx, principal, input)
		return nil, output, err
	})
}

func registerUnavailableRecentToolCallTools(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "list_recent_tool_calls",
		Title:       "List Recent Tool Calls",
		Description: "List recent Tool Logs summaries for one project. This is not switched on for your organization yet.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("recent_tool_calls"))
}
