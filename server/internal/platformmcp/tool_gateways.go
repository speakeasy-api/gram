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
	createGatewayToolName     = "create_gateway"
	setGatewayMembersToolName = "set_gateway_members"

	unavailableGatewayMessage = "Creating or changing gateways is not available on this server."
)

// registerGatewayTools registers the live and unavailable paths with one
// manifest, so the tools never appear on and disappear from the catalogue as
// a deployment composes or fails to compose the service.
func registerGatewayTools(reg *Registrar, service *GatewayService) {
	createHandler := unavailableGatewayHandler[CreateGatewayInput]()
	setHandler := unavailableGatewayHandler[SetGatewayMembersInput]()
	if service.valid() {
		createHandler = func(ctx context.Context, _ *mcp.CallToolRequest, input CreateGatewayInput) (*mcp.CallToolResult, GatewayOutput, error) {
			return principalToolCall(ctx, gatewayToolResult, func(principal Principal) (GatewayOutput, error) {
				return service.CreateGateway(ctx, principal, input)
			})
		}
		setHandler = func(ctx context.Context, _ *mcp.CallToolRequest, input SetGatewayMembersInput) (*mcp.CallToolResult, GatewayOutput, error) {
			return principalToolCall(ctx, gatewayToolResult, func(principal Principal) (GatewayOutput, error) {
				return service.SetGatewayMembers(ctx, principal, input)
			})
		}
	}

	// External only, like the other writes that decide what servers people
	// can reach: an administrator's decision, made on the surface they drive
	// directly.
	meta := ToolMeta{
		Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly,
		ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryMCPWrite,
	}
	addTool(reg, &mcp.Tool{
		Name:  createGatewayToolName,
		Title: "Create a Gateway",
		Description: "Create a gateway in an explicit project: one MCP endpoint that serves the tools of several MCP servers already in that project, each tool named <server slug>--<tool name>. " +
			"Supply a name and the exact MCP server IDs from find_mcp in that same project, in the order the gateway should list them. Before calling, tell the user the project, the name, and the servers it will front. " +
			"The gateway, its default address, and every member are created together, or nothing is: when any server cannot sit behind a gateway, the refusal lists each one with its reason, and the call can be repeated without them. " +
			"The gateway is private, requires sign-in, and is reachable on the public network only; each person signs in to each member's provider through it. When gateway plugin delivery is enabled, eligible gateways are automatically delivered through plugins assigned to roles with Use access. Gateways containing Platform tools are excluded from automatic delivery. Use distribute_mcp_to_plugin for explicit delivery to another plugin. " +
			"Each call creates a new gateway, so after a timeout check find_mcp before calling again. Change its members later with set_gateway_members.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: false, DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, meta, createHandler)
	addTool(reg, &mcp.Tool{
		Name:  setGatewayMembersToolName,
		Title: "Set a Gateway's Members",
		Description: "Replace the MCP servers behind one gateway in an explicit project with exactly the listed servers, in order: servers not yet on it are added, servers left out are removed, and the rest are reordered. An empty list removes every member. " +
			"Before calling, tell the user which servers will be added and removed. When the gateway is in a plugin, the change reaches everyone holding that plugin; an organization administrator adding a server here approves it in Shadow MCP for that audience, as distributing it would. " +
			"The change is applied together or not at all: when any added server cannot sit behind a gateway, the refusal lists each one with its reason. Calling again with the same list changes nothing and reports unchanged.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false)},
	}, meta, setHandler)
}

func unavailableGatewayHandler[In any]() mcp.ToolHandlerFor[In, GatewayOutput] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, GatewayOutput, error) {
		payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: gatewaysFeature, Message: unavailableGatewayMessage})
		if err != nil {
			return nil, GatewayOutput{}, fmt.Errorf("encode unavailable gateway result: %w", err)
		}
		return nil, GatewayOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
}

type gatewayRefusal struct {
	Code    string                 `json:"code"`
	Feature string                 `json:"feature"`
	Message string                 `json:"message"`
	Members []GatewayMemberRefusal `json:"members,omitempty"`
}

func gatewayToolResult(err error) (*mcp.CallToolResult, bool) {
	if refusal, ok := externalAuthorizationToolResult(err); ok {
		return refusal, true
	}
	gatewayErr, ok := errors.AsType[*GatewayError](err)
	if !ok {
		return nil, false
	}
	payload, marshalErr := json.Marshal(gatewayRefusal{Code: gatewayErr.Code, Feature: gatewaysFeature, Message: gatewayErr.Message, Members: gatewayErr.Members})
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
}
