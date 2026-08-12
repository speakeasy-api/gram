//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerListProjectMCPsTool(server *mcp.Server, reader Reader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_project_mcps",
		Title:       "List Project MCPs",
		Description: "List configured MCPs for one explicit project. This does not return secrets, endpoint configuration, or runtime credentials.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListProjectMCPsInput) (*mcp.CallToolResult, ListProjectMCPsOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, ListProjectMCPsOutput{}, err
		}
		if input.ProjectID == "" {
			return nil, ListProjectMCPsOutput{}, fmt.Errorf("project_id is required")
		}
		if reader == nil {
			return nil, ListProjectMCPsOutput{}, ErrUnavailable
		}
		input.Limit = boundedLimit(input.Limit)
		output, err := reader.ListProjectMCPs(ctx, principal, input)
		if err != nil {
			return nil, ListProjectMCPsOutput{}, fmt.Errorf("list project mcps: %w", err)
		}
		return nil, output, nil
	})
}
