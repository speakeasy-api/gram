//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetMCPReadinessToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit Gram project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_catalog_mcp"`
	Force          bool   `json:"force,omitempty" jsonschema:"force one authenticated provider readiness probe; limited to once per minute for this registration"`
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
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit Gram project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_catalog_mcp"`
}

type GetMCPRepairPlanToolOutput struct {
	ProjectSlug    string         `json:"project_slug"`
	RegistrationID string         `json:"registration_id"`
	State          ReadinessState `json:"state"`
	Freshness      string         `json:"freshness"`
	Actions        []RepairAction `json:"actions"`
}

func registerReadinessTools(server *mcp.Server, readiness *ReadinessService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_mcp_readiness",
		Title:       "Get MCP Readiness",
		Description: "Return normalized authenticated readiness for one reviewed MCP registration. A forced probe is limited to once per minute for that registration.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPReadinessToolInput) (*mcp.CallToolResult, GetMCPReadinessToolOutput, error) {
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
		if !found {
			return nil, GetMCPReadinessToolOutput{}, ErrReadinessInvalid
		}
		return nil, readinessToolOutput(project.Slug, input.RegistrationID, result, true), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_mcp_repair_plan",
		Title:       "Get MCP Repair Plan",
		Description: "Return safe, bounded next actions for one reviewed MCP registration from its latest authenticated readiness evidence.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPRepairPlanToolInput) (*mcp.CallToolResult, GetMCPRepairPlanToolOutput, error) {
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
