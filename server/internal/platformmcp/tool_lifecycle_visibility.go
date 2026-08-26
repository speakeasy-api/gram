//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type UpdateMCPVisibilityToolInput struct {
	ProjectSlug     string `json:"project_slug" jsonschema:"explicit project slug that owns the Platform-managed MCP"`
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
			title:       "Turn Off an MCP Server",
			description: "Turn off one fully set-up MCP server, so people stop being able to use it. It stays in the project and every plugin that carries it keeps carrying it; no plugin is created or removed.",
			call:        registrations.DisableMCP,
		},
		{
			name:        "enable_mcp",
			title:       "Turn On an MCP Server",
			description: "Turn one disabled MCP server back on. It is not added to any plugin; the plugins that already carry it are republished, so the people it is shared with get it again. Constraints: connected external callers get a fresh authenticated check, while a managed project assistant returns its own stored evidence instead, because it has no OAuth connection. On an idempotent retry, the result reports current stored visibility while the check and the republish remain best-effort observations.",
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
		{"disable_mcp", "Turn Off an MCP Server", "Turn off one MCP server. This is not switched on for your organization yet."},
		{"enable_mcp", "Turn On an MCP Server", "Turn one MCP server back on. This is not switched on for your organization yet."},
	} {
		addTool(reg, &mcp.Tool{Name: tool.name, Title: tool.title, Description: tool.description}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("mcp_lifecycle_visibility"))
	}
}
