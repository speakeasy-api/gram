//nolint:exhaustruct // MCP manifests intentionally rely on optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerShadowDecisionTool(reg *Registrar, service *ShadowDecisionService) {
	description := "Record and enforce an allow or deny decision for one Shadow MCP target after reviewing it. Requires the current opaque decision version, a stable idempotency key, and explicit confirmation. Allow requires one or more opaque audience references from list_plugin_assignments, including the explicit Everyone reference for organization-wide access; deny accepts none. The committed decision, frozen evidence, enforcement grants, legacy-request drain, audit, and receipt are atomic."
	if service == nil || !service.valid() {
		description = "Decide one Shadow MCP access review. This is not switched on for your organization yet."
		addTool(reg, &mcp.Tool{Name: operationDecideShadowMCPAccess, Title: "Decide Shadow MCP Access", Description: description, Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true}}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, unavailableShadowDecisionTool)
		return
	}
	addTool(reg, &mcp.Tool{Name: operationDecideShadowMCPAccess, Title: "Decide Shadow MCP Access", Description: description, Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true}}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input DecideShadowMCPAccessInput) (*mcp.CallToolResult, DecideShadowMCPAccessOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, DecideShadowMCPAccessOutput{}, err
		}
		output, err := service.Decide(ctx, principal, input)
		if err == nil {
			return nil, output, nil
		}
		if result, ok := shadowDecisionToolResult(err); ok {
			return result, DecideShadowMCPAccessOutput{}, nil
		}
		return nil, DecideShadowMCPAccessOutput{}, err
	})
}

func unavailableShadowDecisionTool(_ context.Context, _ *mcp.CallToolRequest, _ DecideShadowMCPAccessInput) (*mcp.CallToolResult, DecideShadowMCPAccessOutput, error) {
	return nil, DecideShadowMCPAccessOutput{}, shadowDecisionUnavailable(nil)
}

func shadowDecisionToolResult(err error) (*mcp.CallToolResult, bool) {
	if mutation, ok := errors.AsType[*ShadowDecisionError](err); ok {
		payload, marshalErr := json.Marshal(featureUnavailableResult{Code: mutation.Code, Feature: "shadow_mcp_decisions", Message: mutation.Message})
		if marshalErr != nil {
			return nil, false
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}}, IsError: true}, true
	}
	if budget, ok := operationBudgetToolResult(err); ok {
		return budget, true
	}
	return nil, false
}
