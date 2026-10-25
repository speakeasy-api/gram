package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	listGatewayConnectionsToolName = "list_gateway_connections"
	gatewayConnectionsFeature      = "gateway_connections"
)

// registerGatewayConnectionsTool serves one manifest whether or not the
// session service is composed, so the tool never leaves the catalogue.
func registerGatewayConnectionsTool(reg *Registrar, service *GatewayConnectionsService) {
	handler := func(_ context.Context, _ *mcp.CallToolRequest, _ ListGatewayConnectionsInput) (*mcp.CallToolResult, ListGatewayConnectionsOutput, error) {
		payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: gatewayConnectionsFeature, Message: "Gateway connections are unavailable on this server."})
		if err != nil {
			return nil, ListGatewayConnectionsOutput{}, fmt.Errorf("encode unavailable gateway connections: %w", err)
		}
		return nil, ListGatewayConnectionsOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
	if service.valid() {
		handler = func(ctx context.Context, _ *mcp.CallToolRequest, input ListGatewayConnectionsInput) (*mcp.CallToolResult, ListGatewayConnectionsOutput, error) {
			return principalToolCall(ctx, gatewayConnectionsToolResult, func(principal Principal) (ListGatewayConnectionsOutput, error) {
				return service.List(ctx, principal, input)
			})
		}
	}

	// External only: the managed assistant already lists sessions through
	// its own platform tool, scoped to the assistant's project.
	addTool(reg, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name:  listGatewayConnectionsToolName,
		Title: "List a Gateway's Connections",
		Description: "List the sessions people, agents, API keys and workloads hold on one gateway in an explicit project, as the gateway's Debug tab shows them: who is connected, from which client, to which upstream providers, and when each session was created, last used, expires or was revoked. " +
			"Use the gateway_id that create_gateway returns, or that get_plugin returns as target_id for a member whose backend is gateway. Narrow with search and the include and exclude filters; omitting status leaves out revoked sessions. Pages hold 50 sessions unless limit says otherwise, up to 100. " +
			"unattributed_count is how many sessions on the gateway's sign-in server match the same filters but recorded no gateway, so they cannot be listed here. " +
			"Read-only: it never revokes. Never returns tokens, token hashes or credentials. Agents are listed by identity without a name.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}, handler)
}

func gatewayConnectionsToolResult(err error) (*mcp.CallToolResult, bool) {
	result := featureUnavailableResult{Code: "", Feature: gatewayConnectionsFeature, Message: ""}
	shareable, isShareable := errors.AsType[*oops.ShareableError](err)
	switch {
	case errors.Is(err, errGatewayConnectionsInvalid):
		result.Code, result.Message = "invalid_request", fmt.Sprintf("Provide an exact project_id and gateway_id, a search of at most %d characters, and a limit from 1 to %d.", maxGatewayConnectionsSearchLength, maxGatewayConnectionsLimit)
	case isShareable && shareable.Code == oops.CodeBadRequest:
		result.Code, result.Message = "invalid_request", shareable.Error()
	case isShareable && shareable.Code == oops.CodeNotFound:
		result.Code, result.Message = "not_found", "That gateway is not in the selected project."
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{
		Meta: nil, Content: []mcp.Content{&mcp.TextContent{Text: string(content), Meta: nil, Annotations: nil}},
		StructuredContent: nil, IsError: true, InputRequests: nil, RequestState: "",
	}, true
}
