//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The drill-down tools all say the same thing in their descriptions: come here
// after the overview, with the MCP it named. Saying so in the manifest is what
// keeps a model from starting here and paging through occurrences to build the
// summary get_project_overview would have handed it in one call.
const drilldownPreamble = "A closer look at one MCP server that get_project_overview or get_mcp_diagnostics already named. "

func registerDrilldownTools(reg *Registrar, diagnostics *DiagnosticsService) {
	addTool(reg, &mcp.Tool{
		Name:        "query_mcp_events",
		Title:       "MCP Calls by Tool and Outcome",
		Description: drilldownPreamble + "Break its calls down by tool and outcome over a recent window, so a failing server can be narrowed to the tool responsible. Constraints: server-side totals per tool only; there is no free-text filter and no attribute selection.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input QueryMCPEventsInput) (*mcp.CallToolResult, QueryMCPEventsOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, QueryMCPEventsOutput{}, err
		}
		output, err := diagnostics.QueryMCPEvents(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, QueryMCPEventsOutput{}, nil
			}
			return nil, QueryMCPEventsOutput{}, err
		}
		return nil, output, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_mcp_usage_users",
		Title:       "MCP Server Users",
		Description: drilldownPreamble + "List the people observed using it, newest first, with masked identities, categorical activity and error evidence, and short-lived references for a focused follow-up. Constraints: individual call counts and raw identities are never returned.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListMCPUsageUsersInput) (*mcp.CallToolResult, ListMCPUsageUsersOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, ListMCPUsageUsersOutput{}, err
		}
		output, err := diagnostics.ListMCPUsageUsers(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, ListMCPUsageUsersOutput{}, nil
			}
			return nil, ListMCPUsageUsersOutput{}, err
		}
		return nil, output, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "query_mcp_traces",
		Title:       "Recent MCP Calls",
		Description: drilldownPreamble + "List individual calls, newest first, each reduced to a reference you can quote when escalating, plus when it happened, which tool it called, and how it ended. Narrow by outcome. Constraints: this is not a log reader — no arguments, results, bodies, headers, URLs, or identities are returned, and references expire and are bound to this session.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input QueryMCPTracesInput) (*mcp.CallToolResult, QueryMCPTracesOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, QueryMCPTracesOutput{}, err
		}
		output, err := diagnostics.QueryMCPTraces(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, QueryMCPTracesOutput{}, nil
			}
			return nil, QueryMCPTracesOutput{}, err
		}
		return nil, output, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "query_mcp_metrics",
		Title:       "MCP Server Totals",
		Description: drilldownPreamble + "Return its totals over a recent window: call volume, failures, failure rate, average latency, and active users. Constraints: every value is aggregated server-side to the window named in the result; no per-bucket series is returned.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input QueryMCPMetricsInput) (*mcp.CallToolResult, QueryMCPMetricsOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, QueryMCPMetricsOutput{}, err
		}
		output, err := diagnostics.QueryMCPMetrics(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, QueryMCPMetricsOutput{}, nil
			}
			return nil, QueryMCPMetricsOutput{}, err
		}
		return nil, output, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_user_mcp_status",
		Title:       "One Person's MCP Server Status",
		Description: drilldownPreamble + "Use a person reference from list_mcp_usage_users to show which tools that person was observed using and whether errors were observed. Constraints: categorical evidence only, no individual call counts or raw identity; the reference expires and is bound to this session, server, project, and window.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetUserMCPStatusInput) (*mcp.CallToolResult, GetUserMCPStatusOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetUserMCPStatusOutput{}, err
		}
		output, err := diagnostics.GetUserMCPStatus(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, GetUserMCPStatusOutput{}, nil
			}
			return nil, GetUserMCPStatusOutput{}, err
		}
		return nil, output, nil
	})
}

func registerUnavailableDrilldownTools(reg *Registrar) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
	}{
		{"query_mcp_events", "MCP Calls by Tool and Outcome", "Break one MCP server's calls down by tool and outcome. This is not switched on for your organization yet."},
		{"list_mcp_usage_users", "MCP Server Users", "List the masked people observed using one MCP server. This is not switched on for your organization yet."},
		{"get_user_mcp_status", "One Person's MCP Server Status", "Report one referenced person's categorical usage against one MCP server. This is not switched on for your organization yet."},
		{"query_mcp_traces", "Recent MCP Calls", "List recent calls for one MCP server. This is not switched on for your organization yet."},
		{"query_mcp_metrics", "MCP Server Totals", "Return one MCP server's totals. This is not switched on for your organization yet."},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("diagnostics"))
	}
}
