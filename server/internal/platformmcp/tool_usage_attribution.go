//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerSkillUsageTools(reg *Registrar, diagnostics *DiagnosticsService) {
	addTool(reg, &mcp.Tool{
		Name:        "query_skill_usage",
		Title:       "Skill Usage",
		Description: "Summarize observed skill activations for one project, with exact activation counts and privacy-suppressed user counts. An activation proves use, not success or efficacy, so error evidence is reported as not recorded.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input QuerySkillUsageInput) (*mcp.CallToolResult, QuerySkillUsageOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, QuerySkillUsageOutput{}, err
		}
		output, err := diagnostics.QuerySkillUsage(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, QuerySkillUsageOutput{}, nil
			}
			return nil, QuerySkillUsageOutput{}, err
		}
		return nil, output, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_skill_usage_users",
		Title:       "Skill Users",
		Description: "List masked people observed activating one exact skill, with short-lived references for a focused follow-up. Individual activation counts and raw identities are never returned; skill outcomes are not recorded.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListSkillUsageUsersInput) (*mcp.CallToolResult, ListSkillUsageUsersOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, ListSkillUsageUsersOutput{}, err
		}
		output, err := diagnostics.ListSkillUsageUsers(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, ListSkillUsageUsersOutput{}, nil
			}
			return nil, ListSkillUsageUsersOutput{}, err
		}
		return nil, output, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_user_skill_status",
		Title:       "One Person's Skill Status",
		Description: "Use a person reference from list_skill_usage_users to confirm whether that person was observed activating the same skill in the same window. Returns categorical activity only; skill success and errors are not inferred from the surrounding session.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetUserSkillStatusInput) (*mcp.CallToolResult, GetUserSkillStatusOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetUserSkillStatusOutput{}, err
		}
		output, err := diagnostics.GetUserSkillStatus(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, GetUserSkillStatusOutput{}, nil
			}
			return nil, GetUserSkillStatusOutput{}, err
		}
		return nil, output, nil
	})
}

func registerUnavailableSkillUsageTools(reg *Registrar) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
	}{
		{"query_skill_usage", "Skill Usage", "Summarize project skill activations. This is not switched on for your organization yet."},
		{"list_skill_usage_users", "Skill Users", "List masked people observed using one skill. This is not switched on for your organization yet."},
		{"get_user_skill_status", "One Person's Skill Status", "Report one referenced person's categorical skill activity. This is not switched on for your organization yet."},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("skill_usage"))
	}
}
