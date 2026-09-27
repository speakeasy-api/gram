//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerUserSearchTools(reg *Registrar, diagnostics *DiagnosticsService) {
	addTool(reg, &mcp.Tool{
		Name:        "search_users",
		Title:       "Find People in a Project",
		Description: "Find the people observed in one project's telemetry whose identity contains a partial email address or user id, over up to 30 days, most recently seen first. Each match is reduced to a masked identity, categorical activity and error evidence, when they were last seen, and a short-lived person reference for get_user_metrics_summary or search_tool_calls. Constraints: the query must be at least 3 characters, raw identities and individual counts are never returned, pages are bounded, and cursors and references expire and are bound to this session and project.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input SearchUsersInput) (*mcp.CallToolResult, SearchUsersOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, SearchUsersOutput{}, err
		}
		output, err := diagnostics.SearchUsers(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, SearchUsersOutput{}, nil
			}
			return nil, SearchUsersOutput{}, err
		}
		return nil, output, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_user_metrics_summary",
		Title:       "One Person's Activity Summary",
		Description: "Use a person reference from search_users, or from list_mcp_usage_users with the same mcp_id and window, to summarize that person's activity across the project over up to 30 days: call volume, failures, failure rate, the servers their tools name, and their most-failing tools with exact counts. Constraints: the person is reported by masked identity only, no tokens, cost, arguments, or results are returned, and the reference expires and is bound to this session and project.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetUserMetricsSummaryInput) (*mcp.CallToolResult, GetUserMetricsSummaryOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetUserMetricsSummaryOutput{}, err
		}
		output, err := diagnostics.GetUserMetricsSummary(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, GetUserMetricsSummaryOutput{}, nil
			}
			return nil, GetUserMetricsSummaryOutput{}, err
		}
		return nil, output, nil
	})
}

func registerUnavailableUserSearchTools(reg *Registrar) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
	}{
		{"search_users", "Find People in a Project", "Find the masked people observed in one project's telemetry by partial identity. This is not switched on for your organization yet."},
		{"get_user_metrics_summary", "One Person's Activity Summary", "Summarize one referenced person's activity across a project. This is not switched on for your organization yet."},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("user_search"))
	}
}
