//nolint:exhaustruct // MCP manifests intentionally rely on optional zero values.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	operationListShadowAITools = "list_shadow_ai_tools"
	operationListAIScanLibrary = "list_ai_scan_library"
)

func registerShadowAITools(reg *Registrar, service *ShadowAIService) {
	if !service.valid() {
		registerUnavailableShadowAITools(reg)
		return
	}
	addTool(reg, &mcp.Tool{
		Name:        operationListShadowAITools,
		Title:       "List Shadow AI Tools",
		Description: "List the AI tools enrolled devices in this organization have been detected running, with the organization's gateway access decision for each. Covers coding harnesses, general-purpose assistants, and locally run open models; narrow with category. State is allowed, blocked, or unreviewed — a tool that publishes no client ID metadata document always reads unreviewed, because no decision about it can be enforced, and enforceable says which case a row is.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListShadowAIToolsInput) (*mcp.CallToolResult, ListShadowAIToolsOutput, error) {
		return shadowAIToolCall(ctx, func(principal Principal) (ListShadowAIToolsOutput, error) {
			return service.ListTools(ctx, principal, input)
		})
	})
	addTool(reg, &mcp.Tool{
		Name:        operationListAIScanLibrary,
		Title:       "List AI Scan Library",
		Description: "List the AI tools this organization's device agents probe for: the built-in library Speakeasy ships plus any targets the organization added. Says which targets are currently served to agents, which were added by this organization, and which publish a client ID metadata document and are therefore blockable at the gateway. The library version is the number agents echo back once they have received the list.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListAIScanLibraryInput) (*mcp.CallToolResult, ListAIScanLibraryOutput, error) {
		return shadowAIToolCall(ctx, func(principal Principal) (ListAIScanLibraryOutput, error) {
			return service.ListLibrary(ctx, principal, input)
		})
	})
}

func registerUnavailableShadowAITools(reg *Registrar) {
	for _, tool := range []struct{ name, title, description string }{
		{operationListShadowAITools, "List Shadow AI Tools", "List detected AI tools and their gateway access decisions. This is not switched on for your organization yet."},
		{operationListAIScanLibrary, "List AI Scan Library", "List the AI tools device agents probe for. This is not switched on for your organization yet."},
	} {
		addTool(reg, &mcp.Tool{Name: tool.name, Title: tool.title, Description: tool.description, Annotations: readOnlyAnnotations()}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeNone}, unavailableTool(shadowAIFeature))
	}
}

func shadowAIToolCall[Out any](ctx context.Context, call func(Principal) (Out, error)) (*mcp.CallToolResult, Out, error) {
	var zero Out
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	output, err := call(principal)
	if err == nil {
		return nil, output, nil
	}
	if budget, ok := operationBudgetToolResult(err); ok {
		return budget, zero, nil
	}
	return nil, zero, err
}
