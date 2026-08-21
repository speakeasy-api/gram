//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type UpdateMCPMetadataToolInput struct {
	ProjectSlug     string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the Platform-managed MCP"`
	RegistrationID  string `json:"registration_id" jsonschema:"Platform registration ID returned by find_mcp or get_mcp"`
	MCPID           string `json:"mcp_id" jsonschema:"configured MCP ID returned by find_mcp or get_mcp"`
	Name            string `json:"name" jsonschema:"new non-empty project-local MCP display name"`
	ExpectedVersion string `json:"expected_version" jsonschema:"opaque version returned by find_mcp, get_mcp, or a previous update_mcp_metadata result"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"caller-generated idempotency key; reuse only to retry this exact metadata update"`
}

type UpdateMCPMetadataToolOutput struct {
	ProjectSlug    string `json:"project_slug"`
	RegistrationID string `json:"registration_id"`
	MCPID          string `json:"mcp_id"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Visibility     string `json:"visibility"`
	Version        string `json:"version"`
	ReceiptID      string `json:"receipt_id"`
	Replayed       bool   `json:"replayed"`
}

func registerLifecycleMetadataTool(reg *Registrar, registrations *RegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "update_mcp_metadata",
		Title:       "Update MCP Metadata",
		Description: "Rename one complete Platform-managed MCP in an explicit project. This changes no provider configuration, readiness state, plugin attachment, or publication state.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateMCPMetadataToolInput) (*mcp.CallToolResult, UpdateMCPMetadataToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, UpdateMCPMetadataToolOutput{}, err
		}
		result, err := registrations.UpdateMCPMetadata(ctx, principal, UpdateMCPMetadataInput(input))
		if err != nil {
			if toolResult, ok := operationBudgetToolResult(err); ok {
				return toolResult, UpdateMCPMetadataToolOutput{}, nil
			}
			return nil, UpdateMCPMetadataToolOutput{}, err
		}
		return nil, UpdateMCPMetadataToolOutput{
			ProjectSlug: result.Project.Slug, RegistrationID: result.RegistrationID, MCPID: result.MCPID,
			Name: result.Name, Slug: result.Slug, Visibility: result.Visibility, Version: result.Version,
			ReceiptID: result.Receipt.ID.String(), Replayed: result.Receipt.Replayed,
		}, nil
	})
}
