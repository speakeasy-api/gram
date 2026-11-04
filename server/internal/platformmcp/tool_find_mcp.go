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
		Description: "Find readable configured MCPs. Without a query, page the selected or Default project. A query without a project searches the organization: one unique match or up to 10 project-labelled candidates. Built-ins: external admins use list_builtin_mcps/get_builtin_mcp. Hidden servers affect neither paging nor ambiguity. Stored facts only; no remote calls or secrets.",
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
