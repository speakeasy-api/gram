//nolint:exhaustruct_v5 // MCP manifests intentionally rely on optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	operationListShadowAIInventory = "list_shadow_ai_inventory"
	operationListAIScanLibrary     = "list_ai_scan_library"
)

func registerShadowAITools(reg *Registrar, service *ShadowAIService) {
	if !service.valid() {
		registerUnavailableShadowAITools(reg)
		return
	}
	addTool(reg, &mcp.Tool{
		Name:        operationListShadowAIInventory,
		Title:       "List Shadow AI Inventory",
		Description: "List the AI tools enrolled devices in this organization have been detected running, with the organization's gateway access decision for each. Covers coding harnesses, general-purpose assistants, and locally run open models; narrow with category. State is allowed, blocked, or unreviewed — a tool that publishes no client ID metadata document reads unreviewed, because no decision about it can be enforced, and enforceable says which case a row is. catalog_status marks a tool Speakeasy removed from its catalog (removed) or whose sign-in documents are turned off (documents_disabled) while the organization's decision on it still applies; that decision can be changed or cleared but not set anew.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListShadowAIInventoryInput) (*mcp.CallToolResult, ListShadowAIInventoryOutput, error) {
		return principalToolCall(ctx, shadowAIToolResult, func(principal Principal) (ListShadowAIInventoryOutput, error) {
			return service.ListInventory(ctx, reg.aiCatalogSnapshot(ctx), principal, input)
		})
	})
	addTool(reg, &mcp.Tool{
		Name:        operationListAIScanLibrary,
		Title:       "List AI Tools Scanned by Devices",
		Description: "List the AI tools this organization's device agents probe for: the built-in library Speakeasy ships plus any targets the organization added. Says which were added by this organization and which publish a client ID metadata document and are therefore blockable at the gateway. Everything listed is probed for, except a built-in Speakeasy removed from its catalog: it is listed, with catalog_status removed, only while the organization's decision on it still applies. The library version is the number agents echo back once they have received the list.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListAIScanLibraryInput) (*mcp.CallToolResult, ListAIScanLibraryOutput, error) {
		return principalToolCall(ctx, shadowAIToolResult, func(principal Principal) (ListAIScanLibraryOutput, error) {
			return service.ListLibrary(ctx, reg.aiCatalogSnapshot(ctx), principal, input)
		})
	})
}

func registerUnavailableShadowAITools(reg *Registrar) {
	for _, tool := range []struct{ name, title, description string }{
		{operationListShadowAIInventory, "List Shadow AI Inventory", "List detected AI tools and their gateway access decisions. This is not switched on for your organization yet."},
		{operationListAIScanLibrary, "List AI Tools Scanned by Devices", "List the AI tools device agents probe for. This is not switched on for your organization yet."},
	} {
		addTool(reg, &mcp.Tool{Name: tool.name, Title: tool.title, Description: tool.description, Annotations: readOnlyAnnotations()}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, unavailableTool(shadowAIFeature))
	}
}

// shadowAIToolResult projects the service's sentinels into a refusal the model
// can act on, so a caller that named an unknown category or reached a
// deployment without Shadow AI reads a reason rather than a raw handler error.
// Budget refusals are left to operationBudgetToolResult, which is the one place
// rate limits and setup faults are worded.
func shadowAIToolResult(err error) (*mcp.CallToolResult, bool) {
	result := featureUnavailableResult{Feature: shadowAIFeature}
	switch {
	case errors.Is(err, ErrShadowAIInvalid):
		result.Code, result.Message = "invalid_request", "That Shadow AI argument is not one this organization's scan library uses. Narrow with harness, assistant or local_model, or omit the category for all three."
	case errors.Is(err, ErrShadowAIUnavailable):
		result.Code, result.Message = unavailableCode, "Shadow AI is not enabled or is temporarily unavailable."
	default:
		return operationBudgetToolResult(err)
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
