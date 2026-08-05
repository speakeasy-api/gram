//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerListProjectsTool(server *mcp.Server, reader Reader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_projects",
		Title:       "List Projects",
		Description: "List projects in the selected organization. Results contain only project identifiers, names, and slugs.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListProjectsInput) (*mcp.CallToolResult, ListProjectsOutput, error) {
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
