//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerDiagnosticsTools(reg *Registrar, diagnostics *DiagnosticsService) {
	addTool(reg, &mcp.Tool{
		Name:        "get_project_overview",
		Title:       "Project Health Overview",
		Description: "Summarize how one project's MCP servers have been behaving, and what has been failing, over a recent window. Start here for any question about how a project or its MCP servers are doing; only look further once this names a specific server or failure to investigate. Top servers are attributed to the configured MCP server whenever the name the calling app used can be resolved to one, and then carry its mcp_id for use with get_mcp and the diagnostics tools; a name that cannot be resolved is reported as the app used it, with no mcp_id. Constraints: results are aggregated server-side and carry the window they cover, how fresh the underlying observations are, and whether there were any observations at all.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetProjectOverviewInput) (*mcp.CallToolResult, GetProjectOverviewOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetProjectOverviewOutput{}, err
		}
		output, err := diagnostics.GetProjectOverview(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, GetProjectOverviewOutput{}, nil
			}
			return nil, GetProjectOverviewOutput{}, err
		}
		return nil, output, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_mcp_diagnostics",
		Title:       "Diagnose an MCP Server",
		Description: "Work out why one MCP server is not working, using the mcp_id find_mcp or get_mcp returned. Returns the latest server-side check, this server's call outcomes alongside the organization's for comparison, which apps reported failures, and where the fault most likely lies: this platform's configuration, the provider, the app making the calls, or indeterminate. Indeterminate means the evidence does not separate them, and is preferred over a guess. Constraints: no observations is never evidence that the MCP server is healthy.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPDiagnosticsInput) (*mcp.CallToolResult, GetMCPDiagnosticsOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetMCPDiagnosticsOutput{}, err
		}
		output, err := diagnostics.GetMCPDiagnostics(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, GetMCPDiagnosticsOutput{}, nil
			}
			return nil, GetMCPDiagnosticsOutput{}, err
		}
		return nil, output, nil
	})
}
