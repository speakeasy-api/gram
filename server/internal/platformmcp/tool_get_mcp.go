//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetMCPToolPagingNote is shared by every surface that serves get_mcp, so the
// paging contract reads the same wherever an agent meets it.
const GetMCPToolPagingNote = "When tool_exposure.next_tool_cursor is present, tool_exposure lists only part of the server's tools and carries no exposure_version: call again with tool_cursor set to it until it is absent."

func registerGetMCPTool(reg *Registrar, reader Reader) {
	addTool(reg, &mcp.Tool{
		Name:        "get_mcp",
		Title:       "Get One MCP Server",
		Description: "Get a summary of one MCP server already set up in a named project. " + GetMCPToolPagingNote,
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
			// A stale or foreign tool_cursor is the caller's to correct, so it
			// comes back as a readable refusal rather than a tool failure. It
			// is returned as an error, not a result: a result would be checked
			// against get_mcp's output schema, which an empty MCP does not meet.
			if exposure, ok := errors.AsType[*MCPToolExposureError](err); ok {
				if result, handled := toolExposureToolResult(err); handled {
					if refusal, ok := refusalFromResult(result); ok {
						refusal.Code = exposure.Code
						return nil, MCP{}, refusal
					}
				}
			}
			return nil, MCP{}, fmt.Errorf("get configured mcp: %w", err)
		}
		return nil, output, nil
	})
}
