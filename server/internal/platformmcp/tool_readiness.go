//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetMCPReadinessToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_catalog_mcp or register_remote_mcp"`
	Force          bool   `json:"force,omitempty" jsonschema:"force one authenticated provider readiness probe; limited to three probes per minute for this registration; unavailable to managed project assistants"`
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
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_catalog_mcp or register_remote_mcp"`
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
		Title:       "Check If an MCP Server Is Working",
		Description: "Say whether one MCP server is working, from the last stored check, when its registration ID is known. Constraints: forcing a fresh authenticated check is limited to three per minute for that MCP server, and is unavailable to managed project assistants.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{
		// Assistants only read their own persisted, actor-scoped evidence. A
		// forced provider probe stays external because it requires a connection.
		Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPReadinessToolInput) (*mcp.CallToolResult, GetMCPReadinessToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetMCPReadinessToolOutput{}, err
		}
		if principal.surface() == SurfaceProjectAssistant && input.Force {
			result, _ := operationBudgetToolResult(ErrReadinessInvalid)
			return result, GetMCPReadinessToolOutput{}, nil
		}
		var project ResolvedProject
		var result Readiness
		var found bool
		if principal.surface() == SurfaceProjectAssistant {
			project, result, found, err = readiness.CurrentReadiness(ctx, principal, input.ProjectSlug, input.RegistrationID)
		} else {
			project, result, found, err = readiness.GetReadiness(ctx, principal, input.ProjectSlug, input.RegistrationID, input.Force)
		}
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
		Title:       "What to Fix on an MCP Server",
		Description: "List the safe next actions for one MCP server that is not working, from the last stored check, when its registration ID is known. Constraints: managed project assistants read only their own actor-scoped evidence.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{
		// Assistants receive a repair projection from their persisted,
		// actor-scoped evidence; no provider probe or OAuth connection is used.
		Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPRepairPlanToolInput) (*mcp.CallToolResult, GetMCPRepairPlanToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetMCPRepairPlanToolOutput{}, err
		}
		var project ResolvedProject
		var result Readiness
		var found bool
		if principal.surface() == SurfaceProjectAssistant {
			project, result, found, err = readiness.CurrentReadiness(ctx, principal, input.ProjectSlug, input.RegistrationID)
		} else {
			project, result, found, err = readiness.GetRepairPlan(ctx, principal, input.ProjectSlug, input.RegistrationID)
		}
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
