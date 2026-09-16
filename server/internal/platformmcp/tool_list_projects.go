//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerListProjectsTool(reg *Registrar, reader Reader) {
	addTool(reg, &mcp.Tool{
		Name:        "list_projects",
		Title:       "List Projects",
		Description: "List the projects in this organization that the caller may read. A project is where MCP servers and skills are kept. Constraints: results carry only project identifiers, names, and slugs; filtered is true when other organization projects were withheld by RBAC; if truncated is true, stop and use the dashboard to choose from the complete project list.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListProjectsInput) (*mcp.CallToolResult, ListProjectsOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, ListProjectsOutput{}, err
		}
		if reader == nil {
			return nil, ListProjectsOutput{}, ErrUnavailable
		}
		input.Limit = boundedLimit(input.Limit)
		output, err := reader.ListProjects(ctx, principal, input)
		if err != nil {
			return nil, ListProjectsOutput{}, fmt.Errorf("list projects: %w", err)
		}
		return nil, output, nil
	})
}
