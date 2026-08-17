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
		Title:       "Get MCP",
		Description: "Get an allowlisted summary of one configured MCP in an explicit project.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMCPInput) (*mcp.CallToolResult, MCP, error) {
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
		return nil, output, nil
	})
}
