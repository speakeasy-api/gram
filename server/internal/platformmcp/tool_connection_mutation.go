//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	setMCPAddressToolName       = "set_mcp_address"
	setMCPNetworkAccessToolName = "set_mcp_network_access"
)

func registerMCPConnectionMutationTools(reg *Registrar, service *MCPConnectionMutationService) {
	setAddress := unavailableMCPConnectionMutationTool[SetMCPAddressInput, MCPConnectionMutationOutput]()
	setNetworkAccess := unavailableMCPConnectionMutationTool[SetMCPNetworkAccessInput, MCPConnectionMutationOutput]()
	if service != nil {
		setAddress = func(ctx context.Context, _ *mcp.CallToolRequest, input SetMCPAddressInput) (*mcp.CallToolResult, MCPConnectionMutationOutput, error) {
			return principalToolCall(ctx, mcpConnectionMutationToolResult, func(principal Principal) (MCPConnectionMutationOutput, error) {
				return service.SetAddress(ctx, principal, input)
			})
		}
		setNetworkAccess = func(ctx context.Context, _ *mcp.CallToolRequest, input SetMCPNetworkAccessInput) (*mcp.CallToolResult, MCPConnectionMutationOutput, error) {
			return principalToolCall(ctx, mcpConnectionMutationToolResult, func(principal Principal) (MCPConnectionMutationOutput, error) {
				return service.SetNetworkAccess(ctx, principal, input)
			})
		}
	}
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}
	addTool(reg, &mcp.Tool{
		Name: setMCPAddressToolName, Title: "Set MCP Address",
		Description: "Create or update the address for one exact MCP server or gateway in an explicit project. Supply the exact settings version from get_mcp_connection_settings, an idempotency key, and confirmed: true only after the user confirms the exact change. This commits the local desired address and requests publication; the response reports the publication request outcome, not that publication or downstream convergence has completed.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true)},
	}, meta, setAddress)
	addTool(reg, &mcp.Tool{
		Name: setMCPNetworkAccessToolName, Title: "Set MCP Network Access",
		Description: "Change network access for one exact MCP server or gateway in an explicit project to public_only, dual, or private_only. Supply the exact settings version from get_mcp_connection_settings, an idempotency key, and confirmed: true only after the user confirms the exact change. This commits the local desired mode and requests publication; the response reports the publication request outcome, not that publication or downstream convergence has completed. Private access may be rejected when it is unavailable for the organization.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true)},
	}, meta, setNetworkAccess)
}

func unavailableMCPConnectionMutationTool[In, Out any]() mcp.ToolHandlerFor[In, Out] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: "mcp_connection_mutations", Message: "MCP connection mutations are unavailable on this server."})
		if err != nil {
			return nil, zero, fmt.Errorf("encode unavailable MCP connection mutation result: %w", err)
		}
		return nil, zero, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
}

func mcpConnectionMutationToolResult(err error) (*mcp.CallToolResult, bool) {
	if refusal, ok := externalAuthorizationToolResult(err); ok {
		return refusal, true
	}
	result := featureUnavailableResult{Feature: "mcp_connection_mutations"}
	switch {
	case errors.Is(err, ErrMCPConnectionMutationInvalid):
		result.Code = "invalid_request"
		result.Message = "The connection change is invalid or was not explicitly confirmed. Read the latest settings and confirm the exact requested change."
	case errors.Is(err, ErrMCPConnectionMutationConflict):
		result.Code = "conflict"
		result.Message = "The connection settings changed or the address is already in use. Read the latest settings before retrying."
	case errors.Is(err, ErrMCPConnectionMutationMissing):
		result.Code = "not_found"
		result.Message = "That target is not available in the selected project."
	case errors.Is(err, ErrUnavailable):
		result.Code = unavailableCode
		result.Message = "MCP connection mutations are temporarily unavailable."
	default:
		var mutation *RiskMutationError
		if !errors.As(err, &mutation) {
			return nil, false
		}
		result.Code, result.Message = mutation.Code, mutation.Message
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}
