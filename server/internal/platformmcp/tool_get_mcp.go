//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerGetMCPTool(reg *Registrar, reader Reader) {
	addTool(reg, &mcp.Tool{
		Name:        "get_mcp",
		Title:       "Get One MCP Server",
		Description: "Get a summary of one MCP server already set up in a named project. For a tunneled MCP server, callers who can read the project's tunneled sources also get tunnel.connection_status: connected, inactive or never_connected says whether a tunnel agent is connected to the gateway, and unknown means it could not be read. It is not evidence that the private server behind the agent works, and an absent tunnel block means it was not read, never that the tunnel is offline.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryMCPRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPInput) (*mcp.CallToolResult, MCP, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, MCP{}, err
		}
		if input.ProjectID == "" || input.MCPID == "" {
			return nil, MCP{}, fmt.Errorf("project_id and mcp_id are required")
		}
		if reader == nil {
			return nil, MCP{}, ErrUnavailable
		}
		output, err := reader.GetMCP(ctx, principal, input)
		if err != nil {
			return nil, MCP{}, fmt.Errorf("get configured mcp: %w", err)
		}
		if tunnels, ok := reader.(tunnelStatusReader); ok {
			output.Tunnel = tunnels.TunnelStatus(ctx, principal, output)
		}
		return nil, output, nil
	})
}
