//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const toolUsageSummaryToolName = "get_tool_usage_summary"

func registerToolUsageSummaryTool(reg *Registrar, diagnostics *DiagnosticsService) {
	addTool(reg, &mcp.Tool{
		Name:        toolUsageSummaryToolName,
		Title:       "Tool Usage by Target Type",
		Description: "Break one project's tool calls down by what they reached over a recent window: hosted MCP servers, tunneled MCP servers, gateways, shadow MCP servers, local tools, and skills. Use it for questions like how much of our usage is shadow MCP versus hosted, or which tunneled MCP servers the team uses. Every target type is reported, zero included, with its share of all calls and its busiest targets; a target that is a configured MCP server carries its mcp_id for use with get_mcp and the diagnostics tools, and calls a calling app reported under one of a configured server's names are counted for that server rather than as shadow MCP. Constraints: results are aggregated server-side from the same read as the dashboard Insights board, carry the window they cover and how fresh the observations are, and never name users, clients, or shadow MCP servers; use list_shadow_mcp_inventory for the reviewed shadow inventory.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetToolUsageSummaryInput) (*mcp.CallToolResult, GetToolUsageSummaryOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetToolUsageSummaryOutput{}, err
		}
		output, err := diagnostics.GetToolUsageSummary(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, GetToolUsageSummaryOutput{}, nil
			}
			return nil, GetToolUsageSummaryOutput{}, err
		}
		return nil, output, nil
	})
}
