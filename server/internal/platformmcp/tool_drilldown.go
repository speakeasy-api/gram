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
const drilldownPreamble = "Bounded drill-down for one MCP that get_project_overview or get_mcp_diagnostics already identified. "

func registerDrilldownTools(reg *Registrar, diagnostics *DiagnosticsService) {
	addTool(reg, &mcp.Tool{
		Name:        "query_mcp_events",
		Title:       "Query MCP Events",
		Description: drilldownPreamble + "Break one MCP's calls down by tool and outcome class over a bounded window, so a failing server can be narrowed to the tool responsible. Returns server-side totals per tool; there is no free-text filter and no attribute selection.",
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
		Name:        "query_mcp_traces",
		Title:       "Query MCP Traces",
		Description: drilldownPreamble + "List individual occurrences, newest first, each reduced to an opaque correlation reference plus when it happened, which tool it called, and how it ended. Narrow with an outcome class. This is not a log reader: it returns no arguments, results, bodies, headers, URLs, or identities. Quote a reference when escalating; references expire and are bound to this session.",
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
		Title:       "Query MCP Metrics",
		Description: drilldownPreamble + "Return one MCP's aggregate levels over a bounded window: call volume, failures, failure rate, average latency, and active users. Every value is aggregated server-side to the window named in the result; no per-bucket series is returned.",
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
		Title:       "Get User MCP Status",
		Description: drilldownPreamble + "Report one subject's state against one MCP, given the opaque subject reference a summary tool returned for them. Returns a masked identity and a state category only — never a raw identifier, a count, or a history. References expire, are bound to this session, and cannot be searched, joined, or constructed; an unknown or expired one is not found.",
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
		{"query_mcp_events", "Query MCP Events", "Break one MCP's calls down by tool and outcome. Diagnostics are not enabled in the current rollout."},
		{"query_mcp_traces", "Query MCP Traces", "List individual occurrences for one MCP. Diagnostics are not enabled in the current rollout."},
		{"query_mcp_metrics", "Query MCP Metrics", "Return one MCP's aggregate levels. Diagnostics are not enabled in the current rollout."},
		{"get_user_mcp_status", "Get User MCP Status", "Report one subject's state against one MCP. Diagnostics are not enabled in the current rollout."},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("diagnostics"))
	}
}
