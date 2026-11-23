//nolint:exhaustruct_v5 // MCP manifests intentionally rely on optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const shadowMCPSettingsFeature = "shadow_mcp_settings"

func registerShadowSettingsTools(reg *Registrar, service *ShadowMCPSettingsService) {
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}
	defaultAccess := &mcp.Tool{
		Name:  operationSetShadowMCPDefaultAccess,
		Title: "Set a Project's Shadow MCP Default Access",
		Description: "Choose what a project does with an MCP server nobody decided on, as the Shadow MCP page's mode switch does. allowed: every server connects unless an administrator denies it; the managed Shadow MCP policy is removed, and denials and official servers still apply. blocked: every server nobody approved is blocked, including servers people use today; it creates or edits the managed Shadow MCP policy, for everyone or, with role references from list_plugin_assignments, only for those roles. " +
			"Refused unchanged when a Guardrails policy holds the setting (default_access.held_by is shadow_ai or guardrails); that policy is changed in Guardrails. Requires default_access.version from list_shadow_mcp_inventory or get_shadow_mcp_review as expected_version and a stable idempotency key; call it with confirmed false to preview the change without writing, then with confirmed true after the user approves. The policy, its audience, the audit record and the receipt commit together, and the result carries the default access read after the write.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true, OpenWorldHint: new(false)},
	}
	officialServer := &mcp.Tool{
		Name:  operationSetShadowMCPOfficialServer,
		Title: "Choose a Shadow MCP Server's Official Server",
		Description: "Choose which project MCP server is the official version of one remote Shadow MCP server, as the Shadow MCP page's Make official sheet does. While a server is official and has an address, agents that connect to the Shadow MCP server directly are blocked wherever a Shadow MCP policy applies and sent to the official server. " +
			"make_official: make mcp_id (from find_mcp, or suggested_official_server.mcp_id) the official version, replacing any automatic choice. keep_shadow: keep using the Shadow MCP server directly, so no server is official, not even one that proxies to its URL. automatic: remove the administrator's choice, so a project MCP server that proxies to the URL is official again when automatic links are on. " +
			"The target is the opaque target_reference from list_shadow_mcp_inventory or get_shadow_mcp_review; local commands cannot have an official server, and the MCP server must be live and turned on in the same project. Requires the target's official_link_version as expected_version and a stable idempotency key; call it with confirmed false to preview the change without writing, then with confirmed true after the user approves. The choice, the audit record and the receipt commit together, and the result carries the target's review read after the write.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true, OpenWorldHint: new(false)},
	}
	if service == nil || !service.valid() {
		defaultAccess.Description = "Set what a project does with an MCP server nobody decided on. This is not switched on for your organization yet."
		officialServer.Description = "Choose a Shadow MCP server's official MCP server. This is not switched on for your organization yet."
		addTool(reg, defaultAccess, meta, unavailableShadowSettingsTool[SetShadowMCPDefaultAccessInput, SetShadowMCPDefaultAccessOutput])
		addTool(reg, officialServer, meta, unavailableShadowSettingsTool[SetShadowMCPOfficialServerInput, SetShadowMCPOfficialServerOutput])
		return
	}
	addTool(reg, defaultAccess, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input SetShadowMCPDefaultAccessInput) (*mcp.CallToolResult, SetShadowMCPDefaultAccessOutput, error) {
		return shadowSettingsToolCall(ctx, func(principal Principal) (SetShadowMCPDefaultAccessOutput, error) {
			return service.SetDefaultAccess(ctx, principal, input)
		})
	})
	addTool(reg, officialServer, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input SetShadowMCPOfficialServerInput) (*mcp.CallToolResult, SetShadowMCPOfficialServerOutput, error) {
		return shadowSettingsToolCall(ctx, func(principal Principal) (SetShadowMCPOfficialServerOutput, error) {
			return service.SetOfficialServer(ctx, principal, input)
		})
	})
}

func shadowSettingsToolCall[Out any](ctx context.Context, call func(Principal) (Out, error)) (*mcp.CallToolResult, Out, error) {
	var zero Out
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	output, err := call(principal)
	if err == nil {
		return nil, output, nil
	}
	if result, ok := shadowWriteToolResult(err, shadowMCPSettingsFeature); ok {
		return result, zero, nil
	}
	return nil, zero, err
}

func unavailableShadowSettingsTool[In, Out any](_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, Out, error) {
	var zero Out
	result, ok := shadowWriteToolResult(shadowDecisionUnavailable(nil), shadowMCPSettingsFeature)
	if !ok {
		return nil, zero, ErrShadowDecisionUnavailable
	}
	return result, zero, nil
}

// shadowWriteToolResult turns a Shadow MCP write refusal into a readable
// tool error result.
func shadowWriteToolResult(err error, feature string) (*mcp.CallToolResult, bool) {
	if mutation, ok := errors.AsType[*ShadowDecisionError](err); ok {
		payload, marshalErr := json.Marshal(featureUnavailableResult{Code: mutation.Code, Feature: feature, Message: mutation.Message})
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
