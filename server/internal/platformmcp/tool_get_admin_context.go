//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package adminmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerGetAdminContextTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_admin_context",
		Title:       "Get Admin Context",
		Description: "Show the organization and connection bound to this Admin MCP session.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, AdminContext, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, AdminContext{}, err
		}
		return nil, AdminContext{
			OrganizationID: principal.OrganizationID,
			ConnectionID:   principal.ConnectionID,
			ReadOnly:       true,
		}, nil
	})
}
