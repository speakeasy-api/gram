//nolint:exhaustruct_v5 // MCP manifests intentionally rely on optional zero values.
package platformmcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const shadowMCPDecisionsFeature = "shadow_mcp_decisions"

func registerShadowDecisionTool(reg *Registrar, service *ShadowDecisionService) {
	description := "Record and enforce an allow or deny decision for one Shadow MCP target after reviewing it. Requires the current opaque decision version, a stable idempotency key, and explicit confirmation. Allow requires one or more opaque audience references from list_plugin_assignments, including the explicit Everyone reference for organization-wide access. Deny blocks everyone when it names no audience, or only the roles or groups whose references it names; a project that blocks servers by default for everyone accepts only a deny for everyone. The committed decision, frozen evidence, enforcement grants, legacy-request drain, audit, and receipt are atomic."
	if service == nil || !service.valid() {
		description = "Decide one Shadow MCP access review. This is not switched on for your organization yet."
		addTool(reg, &mcp.Tool{Name: operationDecideShadowMCPAccess, Title: "Allow or Block a Shadow MCP Server", Description: description, Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true, OpenWorldHint: new(false)}}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, unavailableShadowDecisionTool)
		return
	}
	addTool(reg, &mcp.Tool{Name: operationDecideShadowMCPAccess, Title: "Allow or Block a Shadow MCP Server", Description: description, Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true, OpenWorldHint: new(false)}}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input DecideShadowMCPAccessInput) (*mcp.CallToolResult, DecideShadowMCPAccessOutput, error) {
		return shadowWriteToolCall(ctx, shadowMCPDecisionsFeature, func(principal Principal) (DecideShadowMCPAccessOutput, error) {
			return service.Decide(ctx, principal, input)
		})
	})
}

func unavailableShadowDecisionTool(_ context.Context, _ *mcp.CallToolRequest, _ DecideShadowMCPAccessInput) (*mcp.CallToolResult, DecideShadowMCPAccessOutput, error) {
	result, ok := shadowDecisionToolResult(shadowDecisionUnavailable(nil))
	if !ok {
		return nil, DecideShadowMCPAccessOutput{}, ErrShadowDecisionUnavailable
	}
	return result, DecideShadowMCPAccessOutput{}, nil
}

func shadowDecisionToolResult(err error) (*mcp.CallToolResult, bool) {
	return shadowWriteToolResult(err, shadowMCPDecisionsFeature)
}
