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
// identical, so creating a plugin, renaming it, and turning its auto sync on or
// off never appear on and disappear from the catalogue as a deployment
// composes or fails to compose the writes behind them.
//
// All three stay off the managed assistant like every other plugin write: they are
// confirmed administrator actions, and a plugin created in the organization's
// default project reaches every member as soon as anything is put in it.
func registerPluginMetadataTools(reg *Registrar, plugins *PluginsService) {
	createPlugin := unavailablePluginMetadataHandler[CreatePluginInput](pluginMetadataUnavailableMessage)
	renamePlugin := unavailablePluginMetadataHandler[RenamePluginInput](pluginMetadataUnavailableMessage)
	setPluginAutoSync := unavailablePluginMetadataHandler[SetPluginAutoSyncInput](pluginAutoSyncUnavailableMessage)
	if plugins.autoSyncMutationValid() {
		setPluginAutoSync = func(ctx context.Context, _ *mcp.CallToolRequest, input SetPluginAutoSyncInput) (*mcp.CallToolResult, PluginMetadataMutationOutput, error) {
			return principalToolCall(ctx, pluginMetadataToolResult, func(principal Principal) (PluginMetadataMutationOutput, error) {
				return plugins.SetPluginAutoSync(ctx, principal, input)
			})
		}
	}
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
			"Its auto sync is on unless auto_sync_enabled is false: pass false only when the user wants the plugin's changes to wait until someone syncs by hand, and say that it then stays out of the marketplace until someone does. " +
			"The slug is the plugin's permanent install name. When omitted it is derived from the name; when supplied it must already be at most 60 lowercase letters, digits, and hyphens. Choose one idempotency key for this create and pass it on both calls. Call first without confirmed: true: nothing is created and no receipt is stored, and the confirmation_required refusal returns the exact slug in its slug field. Show the user that slug, never one you worked out yourself, then call again with the same input, the same idempotency key, and confirmed: true once they confirm. Use a fresh key only for a new attempt, such as a different slug after slug_taken. A slug another plugin in the project holds is refused as slug_taken; reusing an idempotency key with different input is refused as idempotency_key_reused. " +
			"The new plugin carries nothing. In the organization's default project it is delivered to every member, as the dashboard does there; in any other project it reaches no one until people are assigned to it. " +
			"Retrying with the same idempotency key returns the plugin already created rather than a second one.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, meta, createPlugin)
	addTool(reg, &mcp.Tool{
		Name:  operationRenamePlugin,
		Title: "Rename a Plugin",
		Description: "Change the display name of one exact plugin in an explicit project. Nothing else changes: not its slug or install name, not its description, not which MCP servers and skills it carries, and not who receives it. " +
			"Name the plugin exactly as list_plugins returned it, pass a stable idempotency key, and set confirmed: true only after the user confirms the plugin and its new name. " +
			"People who already installed the plugin see the new name the next time the plugin is published to them; while its auto sync is off, that waits for a manual sync.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, meta, renamePlugin)
	addTool(reg, &mcp.Tool{
		Name:  operationSetPluginAutoSync,
		Title: "Turn a Plugin's Auto Sync On or Off",
		Description: "Turn auto sync on or off for one exact plugin in an explicit project, as the plugin's Marketplace sync switch in the AICP dashboard does. Nothing else about the plugin changes. " +
			"With auto sync on, the plugin's changes publish to the project's marketplace on their own. With it off, they wait until someone syncs by hand with republish_plugin, or Sync changes in the dashboard, and everyone keeps the version last published; a plugin never published stays out of the marketplace until then. " +
			"Read the plugin with get_plugin first, tell the user which of these the change means, then call this with a stable idempotency key and confirmed: true once they confirm the exact plugin and the setting. " +
			"Turning auto sync off is refused as unsynced_changes while the plugin has changes that haven't been synced, and nothing changes: offer to sync them with republish_plugin, then turn auto sync off once get_plugin reports publication_evidence.fresh=true. " +
			"Turning it back on requests a publish that sends the changes held while it was off; check get_plugin later rather than reporting them published. " +
			"Retrying with the same idempotency key returns the change already made.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false), OpenWorldHint: new(false)},
	}, meta, setPluginAutoSync)
}

func unavailablePluginMetadataHandler[In any](message string) mcp.ToolHandlerFor[In, PluginMetadataMutationOutput] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ In) (*mcp.CallToolResult, PluginMetadataMutationOutput, error) {
		payload, err := json.Marshal(featureUnavailableResult{Code: unavailableCode, Feature: pluginMetadataFeature, Message: message})
		if err != nil {
			return nil, PluginMetadataMutationOutput{}, fmt.Errorf("encode unavailable plugin metadata result: %w", err)
		}
		return nil, PluginMetadataMutationOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: string(payload)}
	}
}

// pluginMetadataRefusal is a create, rename, or auto sync refusal. Slug is present only on
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
