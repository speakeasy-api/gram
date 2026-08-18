//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetMCPReadinessToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_platform_mcp_for_project; get_platform_mcp_onboarding_status also resolves the workflow-bound registration and returns its ID"`
	Force          bool   `json:"force,omitempty" jsonschema:"force one authenticated provider readiness probe; limited to three probes per minute for this registration"`
}

type GetMCPReadinessToolOutput struct {
	ProjectSlug    string         `json:"project_slug"`
	RegistrationID string         `json:"registration_id"`
	State          ReadinessState `json:"state"`
	EvidenceCode   string         `json:"evidence_code,omitempty"`
	Freshness      string         `json:"freshness"`
	CheckedAt      string         `json:"checked_at,omitempty"`
	ExpiresAt      string         `json:"expires_at,omitempty"`
	Actions        []RepairAction `json:"actions"`
}

type GetMCPRepairPlanToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_platform_mcp_for_project; get_platform_mcp_onboarding_status also resolves the workflow-bound registration and returns its ID"`
}

type GetMCPRepairPlanToolOutput struct {
	ProjectSlug    string         `json:"project_slug"`
	RegistrationID string         `json:"registration_id"`
	State          ReadinessState `json:"state"`
	Freshness      string         `json:"freshness"`
	Actions        []RepairAction `json:"actions"`
}

func registerReadinessTools(reg *Registrar, readiness *ReadinessService) {
	addTool(reg, &mcp.Tool{
		Name:        "get_mcp_readiness",
		Title:       "Get MCP Readiness",
		Description: "Return normalized authenticated readiness for one reviewed MCP registration when its registration ID is known. For guided onboarding, use get_platform_mcp_onboarding_status, which resolves the workflow-bound registration. A forced probe is limited to three per minute for that registration.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{
		// External-only: readiness probes are connection-scoped, which a
		// connection-less surface cannot satisfy.
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPReadinessToolInput) (*mcp.CallToolResult, GetMCPReadinessToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetMCPReadinessToolOutput{}, err
		}
		project, result, found, err := readiness.GetReadiness(ctx, principal, input.ProjectSlug, input.RegistrationID, input.Force)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, GetMCPReadinessToolOutput{}, nil
			}
			return nil, GetMCPReadinessToolOutput{}, err
		}
		return nil, readinessToolOutput(project.Slug, input.RegistrationID, normalizedReadiness(result, found), found), nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_mcp_repair_plan",
		Title:       "Get MCP Repair Plan",
		Description: "Return safe, bounded next actions for one reviewed MCP registration when its registration ID is known. For guided onboarding, use get_platform_mcp_onboarding_status to resolve the workflow-bound registration.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{
		// External-only: repair plans read connection-scoped readiness, which a
		// connection-less surface cannot satisfy.
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPRepairPlanToolInput) (*mcp.CallToolResult, GetMCPRepairPlanToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetMCPRepairPlanToolOutput{}, err
		}
		project, result, found, err := readiness.GetRepairPlan(ctx, principal, input.ProjectSlug, input.RegistrationID)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, GetMCPRepairPlanToolOutput{}, nil
			}
			return nil, GetMCPRepairPlanToolOutput{}, err
		}
		result = normalizedReadiness(result, found)
		return nil, GetMCPRepairPlanToolOutput{
			ProjectSlug:    project.Slug,
			RegistrationID: input.RegistrationID,
			State:          result.State,
			Freshness:      readinessFreshness(result, found),
			Actions:        repairActions(result.State),
		}, nil
	})
}

func readinessToolOutput(projectSlug, registrationID string, readiness Readiness, found bool) GetMCPReadinessToolOutput {
	return GetMCPReadinessToolOutput{
		ProjectSlug:    projectSlug,
		RegistrationID: registrationID,
		State:          readiness.State,
		EvidenceCode:   readiness.EvidenceCode,
		Freshness:      readinessFreshness(readiness, found),
		CheckedAt:      readinessTimestamp(readiness.CheckedAt),
		ExpiresAt:      readinessTimestamp(readiness.ExpiresAt),
		Actions:        repairActions(readiness.State),
	}
}
