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
		Description: "Add an endpoint to, or change one existing endpoint of, one exact MCP server (hosted, remote, or tunneled) or gateway in an explicit project. Omit endpoint_id to add an endpoint; a target may have several. A custom domain must be the organization's own; a hosted server's must also be verified and active. Call first without confirmed: true: nothing is written, and the confirmation_required refusal returns a preview of the current settings and the exact change. Show it to the user, then call again with the same input, the exact settings version, the same idempotency key, and confirmed: true once they confirm. This commits the local desired address and requests publication; the response reports the publication request outcome, not that publication or downstream convergence has completed.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false)},
	}, meta, setAddress)
	addTool(reg, &mcp.Tool{
		Name: setMCPNetworkAccessToolName, Title: "Set MCP Network Access",
		Description: "Change network access for one exact MCP server (hosted, remote, or tunneled) or gateway in an explicit project to public_only, dual, or private_only. Call first without confirmed: true: nothing is written, and the confirmation_required refusal returns a preview of the current settings and the exact change. Show it to the user, then call again with the same input, the exact settings version, the same idempotency key, and confirmed: true once they confirm. This commits the local desired mode and requests publication; the response reports the publication request outcome, not that publication or downstream convergence has completed. Private access is refused unless the organization may use it and a live network ingress covers every endpoint.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false)},
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

type mcpConnectionMutationPreviewResult struct {
	featureUnavailableResult
	Preview MCPConnectionMutationPreview `json:"preview"`
}

func mcpConnectionMutationToolResult(err error) (*mcp.CallToolResult, bool) {
	if refusal, ok := externalAuthorizationToolResult(err); ok {
		return refusal, true
	}
	if preview, ok := errors.AsType[*connectionMutationPreviewError](err); ok {
		payload, marshalErr := json.Marshal(mcpConnectionMutationPreviewResult{
			featureUnavailableResult: featureUnavailableResult{Code: "confirmation_required", Feature: "mcp_connection_mutations", Message: preview.Error()},
			Preview:                  preview.Preview,
		})
		if marshalErr != nil {
			return nil, false
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
	}
	result := featureUnavailableResult{Feature: "mcp_connection_mutations"}
	var mutation *RiskMutationError
	switch {
	case errors.As(err, &mutation):
		result.Code, result.Message = mutation.Code, mutation.Message
	case errors.Is(err, ErrMCPConnectionMutationInvalid):
		result.Code = "invalid_request"
		result.Message = "The connection change is invalid. Read the latest settings and check the requested change."
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
		return nil, false
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}
