package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	getTunneledMCPSetupHandoffToolName = "get_tunneled_mcp_setup_handoff"
	tunneledMCPSetupFeature            = "tunneled_mcp_setup"
)

const getTunneledMCPSetupHandoffDescription = "Open the dashboard page that sets up an MCP server running on a private network, reachable through a Speakeasy tunnel. " +
	"Omit mcp_id to get the form that adds a new tunneled MCP server; pass an existing tunneled MCP server's mcp_id to get its tunnel agent setup panel. " +
	"Adding tunneled MCP servers is not available to every organization yet: a not_enabled refusal means the dashboard has no add form for this one, so say so and stop rather than sending the user to the dashboard. " +
	"Returns a dashboard link and fixed instructions only: creating the tunnel, revealing or rotating its key, and running the agent all happen in the dashboard and on the user's own infrastructure. " +
	"Constraints: never ask for, accept, or repeat a tunnel key, header value, or other credential in chat; the link does not create anything, so confirm the result afterwards with get_mcp."

func registerTunneledMCPSetupHandoffTool(reg *Registrar, service *TunneledMCPSetupHandoffService) {
	addTool(reg, &mcp.Tool{
		Name:        getTunneledMCPSetupHandoffToolName,
		Title:       "Open Tunneled MCP Setup in the Dashboard",
		Description: getTunneledMCPSetupHandoffDescription,
		Annotations: tunneledMCPSetupHandoffAnnotations(),
		Meta:        nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
	}, ToolMeta{
		Authorization: ExternalAuthorizationOrgAdmin,
		// The link carries the caller to the dashboard, which performs every
		// write under its own session and authorization.
		Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: nil,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetTunneledMCPSetupHandoffInput) (*mcp.CallToolResult, GetTunneledMCPSetupHandoffOutput, error) {
		var zero GetTunneledMCPSetupHandoffOutput
		if service == nil {
			return nil, zero, tunneledMCPSetupRefusal(unavailableCode, "Tunneled MCP setup handoffs are unavailable on this server.")
		}
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, zero, err
		}
		output, err := service.Handoff(ctx, principal, input)
		if err == nil {
			return nil, output, nil
		}
		switch {
		case errors.Is(err, ErrOperationRateLimited), errors.Is(err, ErrOperationBudgetUnavailable):
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, zero, nil
			}
			return nil, zero, err
		case errors.Is(err, ErrTunneledMCPSetupInvalid):
			return nil, zero, tunneledMCPSetupRefusal("invalid_request", "Provide a project ID and, optionally, the exact ID of an existing tunneled MCP server.")
		case errors.Is(err, ErrTunneledMCPSetupForbidden):
			return nil, zero, tunneledMCPSetupRefusal("permission_denied", "Only an organization administrator can set up a tunneled MCP server.")
		case errors.Is(err, ErrTunneledMCPSetupNotFound):
			return nil, zero, tunneledMCPSetupRefusal("not_found", "That project or MCP server is not available to this caller.")
		case errors.Is(err, ErrTunneledMCPSetupNotEnabled):
			return nil, zero, tunneledMCPSetupRefusal("not_enabled", "Adding tunneled MCP servers is not available for this organization yet, so the dashboard has no form for it. Existing tunneled MCP servers can still be set up with their mcp_id.")
		case errors.Is(err, ErrTunneledMCPSetupNotTunneled):
			return nil, zero, tunneledMCPSetupRefusal("not_tunneled", "That MCP server is not reachable through a tunnel. Omit mcp_id to add a new tunneled MCP server instead.")
		case errors.Is(err, ErrUnavailable):
			return nil, zero, tunneledMCPSetupRefusal(unavailableCode, "Tunneled MCP setup handoffs are temporarily unavailable.")
		default:
			return nil, zero, err
		}
	})
}

func tunneledMCPSetupHandoffAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false), DestructiveHint: nil, IdempotentHint: false, Title: ""}
}

func tunneledMCPSetupRefusal(code, message string) error {
	payload, err := json.Marshal(featureUnavailableResult{Code: code, Feature: tunneledMCPSetupFeature, Message: message})
	if err != nil {
		return fmt.Errorf("encode tunneled MCP setup refusal: %w", err)
	}
	return &ToolRefusalError{Code: code, Payload: string(payload)}
}
