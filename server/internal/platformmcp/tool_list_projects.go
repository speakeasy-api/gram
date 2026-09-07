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
		Description: "List the projects in this organization. A project is where an administrator's MCP servers and skills are kept before anyone receives them. Constraints: results carry only project identifiers, names, and slugs.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListProjectsInput) (*mcp.CallToolResult, ListProjectsOutput, error) {
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
