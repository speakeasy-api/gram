//nolint:exhaustruct // MCP manifests intentionally rely on documented optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerShadowInventoryTools(reg *Registrar, service *ShadowInventoryService) {
	if !service.valid() {
		registerUnavailableShadowInventoryTools(reg)
		return
	}
	addTool(reg, &mcp.Tool{
		Name:        "list_shadow_mcp_inventory",
		Title:       "List Shadow MCP Inventory",
		Description: "List privacy-safe observed or requested MCP targets in an explicit project. Pending reviews appear before observed targets on the first page. Raw URLs, local commands, people, principals, policies, evidence, and research traces are never returned; use the short-lived opaque target reference to inspect one review.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListShadowMCPInventoryInput) (*mcp.CallToolResult, ListShadowMCPInventoryOutput, error) {
		return shadowInventoryToolCall(ctx, func(principal Principal) (ListShadowMCPInventoryOutput, error) {
			return service.List(ctx, principal, input)
		})
	})
	addTool(reg, &mcp.Tool{
		Name:        "get_shadow_mcp_review",
		Title:       "Get Shadow MCP Review",
		Description: "Inspect the closed, privacy-safe evidence and review state for one opaque Shadow MCP target from list_shadow_mcp_inventory. Returns aggregate declarations, gaps, advisory counts, and research coverage only; never raw target values, requesters, evidence documents, findings text, citations, or research traces.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetShadowMCPReviewInput) (*mcp.CallToolResult, GetShadowMCPReviewOutput, error) {
		return shadowInventoryToolCall(ctx, func(principal Principal) (GetShadowMCPReviewOutput, error) {
			return service.GetReview(ctx, principal, input)
		})
	})
}

func registerUnavailableShadowInventoryTools(reg *Registrar) {
	for _, tool := range []struct{ name, title, description string }{
		{"list_shadow_mcp_inventory", "List Shadow MCP Inventory", "List privacy-safe Shadow MCP inventory. This is not switched on for your organization yet."},
		{"get_shadow_mcp_review", "Get Shadow MCP Review", "Inspect one privacy-safe Shadow MCP review. This is not switched on for your organization yet."},
	} {
		addTool(reg, &mcp.Tool{Name: tool.name, Title: tool.title, Description: tool.description, Annotations: readOnlyAnnotations()}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, unavailableTool("shadow_mcp_inventory"))
	}
}

func shadowInventoryToolCall[Out any](ctx context.Context, call func(Principal) (Out, error)) (*mcp.CallToolResult, Out, error) {
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
	result := featureUnavailableResult{Feature: "shadow_mcp_inventory"}
	switch {
	case errors.Is(err, ErrShadowInventoryInvalid):
		result.Code, result.Message = "invalid_request", "The Shadow MCP inventory request or cursor is invalid. Restart the list and use its latest opaque references."
	case errors.Is(err, ErrShadowInventoryNotFound):
		result.Code, result.Message = "not_found", "That project or Shadow MCP target is not available to this organization. List the project inventory again."
	case errors.Is(err, ErrShadowInventoryUnavailable):
		result.Code, result.Message = unavailableCode, "Shadow MCP inventory is not enabled or is temporarily unavailable."
	default:
		return nil, zero, err
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, zero, errors.Join(errors.New("encode shadow inventory refusal"), marshalErr)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, zero, nil
}
