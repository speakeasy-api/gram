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
		Title:       "Find MCP Servers",
		Description: "Find MCP servers already set up that the caller may read. Without a query, returns a page from the selected or Default project. With a query and no project named, searches permitted servers across the organization and returns one unique match, or at most 10 candidates each labelled with its project. Constraints: hidden servers do not consume pages or affect ambiguity; results are stored facts only — no remote MCP server is contacted and no secret is returned.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryMCPRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input FindMCPInput) (*mcp.CallToolResult, FindMCPOutput, error) {
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
