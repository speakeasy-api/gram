//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const deleteMCPToolName = "delete_mcp"

const deleteMCPToolDescription = "Delete one exact MCP server from an explicit project, for organization administrators cleaning up servers that are duplicated, unused, or left behind. Unlike disable_mcp, it works on any server in the project, including servers created in the dashboard or by other people. First read the server with find_mcp or get_mcp, tell the user exactly which server in which project will be deleted, and wait for their confirmation; then pass its project_id, mcp_id, the version from that read as expected_version, a new idempotency key, and confirmed: true. If the response is lost, retry with the same idempotency key so a finished delete is reported, not repeated. A remote or tunneled server is removed together with any sign-in it alone used; a server hosted from a toolset is turned off instead, as the dashboard does, and its toolset is unchanged. A deleted server leaves every plugin that carried it; a turned-off hosted server stays in its plugins but no one can use it. This cannot be undone here. A conflict means the server changed since it was read: read it again and renew confirmation. Never delete more than the user named."

func registerLifecycleDeleteTool(reg *Registrar, service *LifecycleDeleteService) {
	handler := unavailableLifecycleDeleteTool
	if service != nil {
		handler = func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteMCPInput) (*mcp.CallToolResult, DeleteMCPOutput, error) {
			return principalToolCall(ctx, deleteMCPToolResult, func(principal Principal) (DeleteMCPOutput, error) {
				return service.Delete(ctx, principal, input)
			})
		}
	}
	// External only: the managed assistant has no confirmation flow for
	// deleting another person's server.
	addTool(reg, &mcp.Tool{
		Name: deleteMCPToolName, Title: "Delete an MCP Server",
		Description: deleteMCPToolDescription,
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true},
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, handler)
}

func unavailableLifecycleDeleteTool(_ context.Context, _ *mcp.CallToolRequest, _ DeleteMCPInput) (*mcp.CallToolResult, DeleteMCPOutput, error) {
	payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: "mcp_lifecycle_delete", Message: "Deleting MCP servers is unavailable on this server. Delete the server in the dashboard instead."})
	if err != nil {
		return nil, DeleteMCPOutput{}, fmt.Errorf("encode unavailable MCP delete result: %w", err)
	}
	return nil, DeleteMCPOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
}

func deleteMCPToolResult(err error) (*mcp.CallToolResult, bool) {
	if refusal, ok := externalAuthorizationToolResult(err); ok {
		return refusal, true
	}
	result := featureUnavailableResult{Feature: "mcp_lifecycle_delete"}
	var mutation *RiskMutationError
	switch {
	case errors.Is(err, ErrLifecycleDeleteMissing):
		result.Code = "not_found"
		result.Message = "No such server exists in that project. Read the project's servers again; nothing is selected by default."
	case errors.As(err, &mutation):
		result.Code, result.Message = mutation.Code, mutation.Message
	default:
		return nil, false
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}
