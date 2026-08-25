package platformmcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerFindMCPTool(reg *Registrar, reader Reader, cursorKeyMaterial string) {
	//nolint:exhaustruct // MCP SDK manifest intentionally relies on documented optional zero values.
	addTool(reg, &mcp.Tool{
		Name:        "find_mcp",
		Title:       "Find MCP",
		Description: "Find configured MCPs. Without a query, returns a bounded page from the selected or Default project and an opaque continuation cursor. With an unscoped query, searches the organization and returns one unique match or at most 10 project-qualified candidates. Results contain persisted allowlisted inventory facts only and never probe remote MCPs or expose secrets.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input FindMCPInput) (*mcp.CallToolResult, FindMCPOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, FindMCPOutput{}, err
		}
		if input.ProjectID != "" && input.ProjectSlug != "" {
			return nil, FindMCPOutput{}, fmt.Errorf("only one of project_id or project_slug may be supplied")
		}
		if reader == nil || cursorKeyMaterial == "" {
			return nil, FindMCPOutput{}, ErrUnavailable
		}
		output, err := reader.FindMCP(ctx, principal, input)
		if err != nil {
			return nil, FindMCPOutput{}, ErrUnavailable
		}
		return nil, output, nil
	})
}
