//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerPluginMetadataTools keeps the live and unavailable manifests
// identical, so creating and renaming a plugin never appear on and disappear
// from the catalogue as a deployment composes or fails to compose the writes
// behind them.
//
// Both stay off the managed assistant like every other plugin write: they are
// confirmed administrator actions, and a plugin created in the organization's
// default project reaches every member as soon as anything is put in it.
func registerPluginMetadataTools(reg *Registrar, plugins *PluginsService) {
	createPlugin := unavailablePluginMetadataHandler[CreatePluginInput]()
	renamePlugin := unavailablePluginMetadataHandler[RenamePluginInput]()
	if plugins.metadataMutationValid() {
		createPlugin = func(ctx context.Context, _ *mcp.CallToolRequest, input CreatePluginInput) (*mcp.CallToolResult, PluginMetadataMutationOutput, error) {
			return principalToolCall(ctx, pluginMetadataToolResult, func(principal Principal) (PluginMetadataMutationOutput, error) {
				return plugins.CreatePlugin(ctx, principal, input)
			})
		}
		renamePlugin = func(ctx context.Context, _ *mcp.CallToolRequest, input RenamePluginInput) (*mcp.CallToolResult, PluginMetadataMutationOutput, error) {
			return principalToolCall(ctx, pluginMetadataToolResult, func(principal Principal) (PluginMetadataMutationOutput, error) {
				return plugins.RenamePlugin(ctx, principal, input)
			})
		}
	}

	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}
	addTool(reg, &mcp.Tool{
		Name:  operationCreatePlugin,
		Title: "Create a Plugin",
		Description: "Create one empty plugin — a bundle of MCP servers and skills shared with people — in an explicit project, so an MCP server or skill can then be put into it. " +
			"Name it, optionally give it a slug and description, and pass a stable idempotency key. " +
			"The slug is the plugin's permanent install name. When omitted it is derived from the name; when supplied it must already be at most 60 lowercase letters, digits, and hyphens. Call first without confirmed: true: nothing is created, and the confirmation_required refusal returns the exact slug in its slug field. Show the user that slug, never one you worked out yourself, then call again with confirmed: true and a fresh idempotency key once they confirm. A slug another plugin in the project holds is refused as slug_taken; reusing an idempotency key with different input is refused as idempotency_key_reused. " +
			"The new plugin carries nothing. In the organization's default project it is delivered to every member, as the dashboard does there; in any other project it reaches no one until people are assigned to it. " +
			"Retrying with the same idempotency key returns the plugin already created rather than a second one.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false)},
	}, meta, createPlugin)
	addTool(reg, &mcp.Tool{
		Name:  operationRenamePlugin,
		Title: "Rename a Plugin",
		Description: "Change the display name of one exact plugin in an explicit project. Nothing else changes: not its slug or install name, not its description, not which MCP servers and skills it carries, and not who receives it. " +
			"Name the plugin exactly as list_plugins returned it, pass a stable idempotency key, and set confirmed: true only after the user confirms the plugin and its new name. " +
			"People who already installed the plugin see the new name the next time the plugin is published to them.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false)},
	}, meta, renamePlugin)
}

func unavailablePluginMetadataHandler[In any]() mcp.ToolHandlerFor[In, PluginMetadataMutationOutput] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, PluginMetadataMutationOutput, error) {
		payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: pluginMetadataFeature, Message: "Creating and renaming plugins is not available on this server."})
		if err != nil {
			return nil, PluginMetadataMutationOutput{}, fmt.Errorf("encode unavailable plugin metadata result: %w", err)
		}
		return nil, PluginMetadataMutationOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
}

// pluginMetadataRefusal is a create or rename refusal. Slug is present only on
// an unconfirmed create, where it is the exact slug the plugin would get.
type pluginMetadataRefusal struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Slug    string `json:"slug,omitempty"`
}

func pluginMetadataToolResult(err error) (*mcp.CallToolResult, bool) {
	if refusal, ok := externalAuthorizationToolResult(err); ok {
		return refusal, true
	}
	if mutation, ok := errors.AsType[*PluginMetadataMutationError](err); ok {
		content, marshalErr := json.Marshal(pluginMetadataRefusal{Code: mutation.Code, Message: mutation.Message, Slug: mutation.Slug})
		if marshalErr != nil {
			return nil, false
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
	}
	return pluginToolResult(err)
}
