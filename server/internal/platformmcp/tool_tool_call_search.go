//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerToolCallSearchTools(reg *Registrar, diagnostics *DiagnosticsService) {
	addTool(reg, &mcp.Tool{
		Name:        "search_tool_calls",
		Title:       "Search Tool Calls",
		Description: "Search one project's tool calls across every MCP server over up to 30 days, newest first. Narrow by tool name text, error text, outcome, one configured MCP server, a person reference, and attribute filters discovered with list_attribute_keys. Each call is reduced to when it happened, the tool and configured server, how it ended, the calling app, a masked identity, and a short-lived person reference that narrows a follow-up search. Constraints: this is not a log reader; no arguments, results, bodies, headers, URLs, trace IDs, or raw identities are returned, pages are bounded, and cursors and references expire and are bound to this session.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input SearchToolCallsInput) (*mcp.CallToolResult, SearchToolCallsOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, SearchToolCallsOutput{}, err
		}
		output, err := diagnostics.SearchToolCalls(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, SearchToolCallsOutput{}, nil
			}
			return nil, SearchToolCallsOutput{}, err
		}
		return nil, output, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_attribute_keys",
		Title:       "List Attribute Keys",
		Description: "List the attribute keys present on one project's telemetry over a window, split into the custom @-prefixed keys its integrations attached and the system keys the platform recorded. Call this before search_tool_calls to learn which attribute filters exist. Constraints: keys only, never values; keys that carry tool content are withheld because a search refuses them.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListAttributeKeysInput) (*mcp.CallToolResult, ListAttributeKeysOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, ListAttributeKeysOutput{}, err
		}
		output, err := diagnostics.ListAttributeKeys(ctx, principal, input)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, ListAttributeKeysOutput{}, nil
			}
			return nil, ListAttributeKeysOutput{}, err
		}
		return nil, output, nil
	})
}

func registerUnavailableToolCallSearchTools(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "search_tool_calls",
		Title:       "Search Tool Calls",
		Description: "Search one project's tool calls across every MCP server. This is not switched on for your organization yet.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("tool_call_search"))
	addTool(reg, &mcp.Tool{
		Name:        "list_attribute_keys",
		Title:       "List Attribute Keys",
		Description: "List the attribute keys present on one project's telemetry. This is not switched on for your organization yet.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}, unavailableTool("tool_call_search"))
}
