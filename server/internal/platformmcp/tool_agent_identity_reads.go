package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	getAgentMCPAccessToolName            = "get_agent_mcp_access"
	listAgentAuthorizationChecksToolName = "list_agent_authorization_checks"
)

// registerAgentIdentityReadTools serves the agent identity reads. A nil
// service keeps both tools listed with the same contract and refuses each call
// as unavailable.
//
// Agents belong to the organization, not to one project, and the managed
// assistant has no person to check agent ownership against, so both tools
// are external only. Any member may call them: an agent's owner need not be
// an administrator, and each handler applies the dashboard's agent checks.
func registerAgentIdentityReadTools(reg *Registrar, service *AgentIdentityReadService) {
	meta := ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeNone, DiscoveryScopes: nil}

	addTool(reg, &mcp.Tool{ //nolint:exhaustruct // Optional SDK fields use documented defaults.
		Name:        getAgentMCPAccessToolName,
		Title:       "See an Agent's MCP Servers",
		Description: "Inspect one agent identity's MCP servers: every server its mcp:connect grants reach across the organization, how the grant reaches it (the server itself, its whole project, or every server), and whose credential the agent signs in with there. credential_mode is shared when the server asks no person to sign in, so the agent uses the server's shared credential and acts for no one; on_behalf_of when it signs in with the people in on_behalf_of, whose own connections are assigned to it (connection_expired marks one that no longer works); not_assigned when the server asks each person to sign in and no one's working connection is assigned, so its calls there cannot sign in. Pass the exact agent_id. Requires the same access as the agent's Permissions tab in the dashboard: its owner, or agent:write on the agent. Names each person by display name, or by email when they have none, and never returns credentials, tokens, or grant IDs. Read-only; change grants and connection assignments in the dashboard.",
		Annotations: readOnlyAnnotations(),
	}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input AgentIdentityInput) (*mcp.CallToolResult, AgentMCPAccessOutput, error) {
		return principalToolCall(ctx, agentIdentityReadToolResult, func(principal Principal) (AgentMCPAccessOutput, error) {
			return service.MCPAccess(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{ //nolint:exhaustruct // Optional SDK fields use documented defaults.
		Name:        listAgentAuthorizationChecksToolName,
		Title:       "List an Agent's Authorization Checks",
		Description: "List one agent identity's recent authorization checks, denied checks first, then allowed ones. Each outcome is grouped by permission and resource with how many times it happened, when last, and why the latest was decided as it was; MCP server resources carry the server's name. total counts every check of that outcome in the period, and truncated says the groups cover only the newest 200. org:hooks_ingest checks, one per hook event the agent sends, are left out of both lists and their totals. The period defaults to the seven days before to (now by default) and spans at most 90 days. Pass the exact agent_id. Requires the same access as the agent's Authorization checks panel in the dashboard: its owner or agent:read on the agent, and org:read. Read-only.",
		Annotations: readOnlyAnnotations(),
	}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input AgentAuthorizationChecksInput) (*mcp.CallToolResult, AgentAuthorizationChecksOutput, error) {
		return principalToolCall(ctx, agentIdentityReadToolResult, func(principal Principal) (AgentAuthorizationChecksOutput, error) {
			return service.AuthorizationChecks(ctx, principal, input)
		})
	})
}

// agentIdentityReadToolResult turns a refusal into a readable result without
// saying whether an agent the caller may not inspect exists.
func agentIdentityReadToolResult(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}
	if result, ok := operationBudgetToolResult(err); ok {
		return result, true
	}
	result := featureUnavailableResult{Code: unavailableCode, Feature: "agent_identity_reads", Message: "Agent identity reads are temporarily unavailable."}
	switch {
	case errors.Is(err, ErrAgentNotAvailable):
		result.Code, result.Message = "not_found", "That agent is not available to you. Check the exact agent ID; reading an agent needs its owner or agent access on it."
	case errors.Is(err, ErrAgentChecksForbidden):
		result.Code, result.Message = "permission_denied", "Listing an agent's authorization checks needs org:read, as the dashboard's checks panel does."
	case errors.Is(err, ErrAgentChecksWindowInvalid):
		result.Code, result.Message = "invalid_request", "Give from and to as RFC3339 timestamps, from before to, at most 90 days apart."
	case errors.Is(err, ErrForbidden):
		result.Code, result.Message = "permission_denied", "Agent identity reads need a signed-in organization member."
	case errors.Is(err, ErrUnavailable):
	default:
		return nil, false
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Meta: nil, StructuredContent: nil, InputRequests: nil, RequestState: "", Content: []mcp.Content{&mcp.TextContent{Meta: nil, Annotations: nil, Text: string(payload)}}, IsError: true}, true
}
