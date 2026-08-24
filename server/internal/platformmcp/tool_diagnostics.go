//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerDiagnosticsTools(reg *Registrar, diagnostics *DiagnosticsService) {
	addTool(reg, &mcp.Tool{
		Name:        "get_project_overview",
		Title:       "Get Project Overview",
		Description: "Summarize one project's MCP activity and failures over a bounded window. This is the normal entry point for any question about how a project or its MCPs are behaving; use the drill-down diagnostics only after this identifies a specific MCP or failure to look into. Results are aggregated server-side and carry the window they were computed over, how fresh the underlying observations are, and whether there were any observations at all.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetProjectOverviewInput) (*mcp.CallToolResult, GetProjectOverviewOutput, error) {
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
		Title:       "Get MCP Diagnostics",
		Description: "Diagnose one configured MCP that is not working, using the mcp_id find_mcp or get_mcp returned. Returns the latest server-side readiness result, this MCP's call outcomes and the organization's for comparison, the clients that reported failures, and an explicit fault attribution: gram_configuration, provider, client, or indeterminate. An indeterminate answer means the evidence does not separate the candidates and is preferred over a guess. No observations is never evidence that the MCP is healthy.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPDiagnosticsInput) (*mcp.CallToolResult, GetMCPDiagnosticsOutput, error) {
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

func registerUnavailableDiagnosticsTools(reg *Registrar) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
	}{
		{"get_project_overview", "Get Project Overview", "Summarize one project's MCP activity and failures. Diagnostics are not enabled in the current rollout."},
		{"get_mcp_diagnostics", "Get MCP Diagnostics", "Diagnose one configured MCP that is not working. Diagnostics are not enabled in the current rollout."},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("diagnostics"))
	}
}
