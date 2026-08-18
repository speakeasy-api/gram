//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerGetPlatformContextTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "get_platform_context",
		Title:       "Get Platform Context",
		Description: "Show the organization and connection bound to this Platform MCP session.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, PlatformContext, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, PlatformContext{}, err
		}
		return nil, PlatformContext{
			OrganizationID: principal.OrganizationID,
			ConnectionID:   principal.ConnectionID,
			ReadOnly:       false,
		}, nil
	})
}
