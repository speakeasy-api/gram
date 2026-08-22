//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type UpdateMCPVisibilityToolInput struct {
	ProjectSlug     string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the Platform-managed MCP"`
	RegistrationID  string `json:"registration_id" jsonschema:"Platform registration ID returned by find_mcp or get_mcp"`
	MCPID           string `json:"mcp_id" jsonschema:"configured MCP ID returned by find_mcp or get_mcp"`
	ExpectedVersion string `json:"expected_version" jsonschema:"opaque version returned by find_mcp, get_mcp, or a previous lifecycle update"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"caller-generated idempotency key; reuse only to retry this exact visibility update"`
}

type UpdateMCPVisibilityToolOutput struct {
	ProjectSlug    string       `json:"project_slug"`
	RegistrationID string       `json:"registration_id"`
	MCPID          string       `json:"mcp_id"`
	Visibility     string       `json:"visibility"`
	Version        string       `json:"version"`
	ReceiptID      string       `json:"receipt_id"`
	Replayed       bool         `json:"replayed"`
	Readiness      MCPReadiness `json:"readiness"`
	Published      bool         `json:"published"`
}

func registerLifecycleVisibilityTools(reg *Registrar, registrations *RegistrationService) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
		call        func(context.Context, Principal, UpdateMCPVisibilityInput) (UpdateMCPVisibilityResult, error)
	}{
		{
			name:        "disable_mcp",
			title:       "Disable MCP",
			description: "Disable one complete Platform-managed MCP. The MCP remains registered and every existing plugin attachment remains unchanged; no plugin is created or removed.",
			call:        registrations.DisableMCP,
		},
		{
			name:        "enable_mcp",
			title:       "Enable MCP",
			description: "Re-enable one disabled Platform-managed MCP. The MCP is not attached to any plugin; existing attachments are republished and a fresh readiness probe runs. On an idempotent retry, the result reports current persisted visibility while readiness and publication remain best-effort observations.",
			call:        registrations.EnableMCP,
		},
	} {
		addTool(reg, &mcp.Tool{Name: tool.name, Title: tool.title, Description: tool.description}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateMCPVisibilityToolInput) (*mcp.CallToolResult, UpdateMCPVisibilityToolOutput, error) {
			principal, err := principalFromToolContext(ctx)
			if err != nil {
				return nil, UpdateMCPVisibilityToolOutput{}, err
			}
			result, err := tool.call(ctx, principal, UpdateMCPVisibilityInput(input))
			if err != nil {
				if toolResult, ok := operationBudgetToolResult(err); ok {
					return toolResult, UpdateMCPVisibilityToolOutput{}, nil
				}
				return nil, UpdateMCPVisibilityToolOutput{}, err
			}
			return nil, UpdateMCPVisibilityToolOutput{
				ProjectSlug: result.Project.Slug, RegistrationID: result.RegistrationID, MCPID: result.MCPID,
				Visibility: result.Visibility, Version: result.Version, ReceiptID: result.Receipt.ID.String(), Replayed: result.Receipt.Replayed,
				Readiness: result.Readiness, Published: result.Published,
			}, nil
		})
	}
}

func registerUnavailableLifecycleVisibilityTools(reg *Registrar) {
	for _, tool := range []struct{ name, title, description string }{
		{"disable_mcp", "Disable MCP", "Disable one Platform-managed MCP. Visibility controls are not available in the current preview."},
		{"enable_mcp", "Enable MCP", "Enable one Platform-managed MCP. Visibility controls are not available in the current preview."},
	} {
		addTool(reg, &mcp.Tool{Name: tool.name, Title: tool.title, Description: tool.description}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("mcp_lifecycle_visibility"))
	}
}
