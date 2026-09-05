//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type pluginRefusalResult struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Plugin inventory is externalOnly: the managed assistant already reads a
// project's plugins through platform_list_plugins in its own tool catalog, and
// admitting a second name for the same capability would make the assistant
// choose between two tools that answer the same question.
func registerPluginTools(reg *Registrar, plugins *PluginsService) {
	setDescription := "Replace the complete assignment set of one exact plugin when assignment changes are enabled for its project. First read the plugin and current assignments. If this capability is available, explain the complete replacement and publication state, ask the user to confirm it, then call this with confirmed: true. Constraints: pass the assignment version from get_plugin, only opaque assignment references from get_plugin or list_plugin_assignments, and a stable idempotency key. An empty set removes every assignment and reaches nobody. This changes who can discover an already-published package; it does not publish package bytes. If the project is not enabled, this returns feature_unavailable without changing anything."
	addTool(reg, &mcp.Tool{
		Name:        operationSetPluginAssignments,
		Title:       "Set Plugin Assignments",
		Description: setDescription,
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input SetPluginAssignmentsInput) (*mcp.CallToolResult, SetPluginAssignmentsOutput, error) {
		return pluginToolCall(ctx, func(principal Principal) (SetPluginAssignmentsOutput, error) {
			return plugins.SetPluginAssignments(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_plugin_assignments",
		Title:       "List Plugin Assignments",
		Description: "List up to 100 existing roles and directory assignment targets that can receive plugins in an explicit project. Each assignment has a short-lived opaque reference and, where available, a privacy-safe member count; Everyone has no member count. Raw principal identifiers are never returned. If truncated is true, use the dashboard to choose from the complete assignment set.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListPluginAssignmentsInput) (*mcp.CallToolResult, ListPluginAssignmentsOutput, error) {
		return pluginToolCall(ctx, func(principal Principal) (ListPluginAssignmentsOutput, error) {
			return plugins.ListPluginAssignments(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_plugins",
		Title:       "List Plugins",
		Description: "List the plugins in a named project. A plugin is the bundle of MCP servers and skills an administrator shares with people, so this is the level to answer \"what do we ship\" at, rather than adding up individual servers. Each entry reports how much the plugin carries, who receives it, and whether it has been published — that is, whether the people it is shared with have it yet.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListPluginsInput) (*mcp.CallToolResult, ListPluginsOutput, error) {
		return principalToolCall(ctx, pluginToolResult, func(principal Principal) (ListPluginsOutput, error) {
			return plugins.ListPlugins(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_plugin",
		Title:       "Get One Plugin",
		Description: "Get one plugin — the bundle of MCP servers and skills you share with people — and what it carries: its MCP servers, skills, up to 100 current assignments, and an assignment version for safe follow-up edits. Assignment references expire at the returned time; if assignments_truncated is true or assignment_details_complete is false, use the dashboard before editing assignments. The general truncated field applies only to MCP servers and skills. Constraints: name the plugin exactly by ID, slug, or name; a name matching nothing is refused as not_found and a name matching more than one plugin as ambiguous_target, never silently answered with the default plugin.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetPluginInput) (*mcp.CallToolResult, GetPluginOutput, error) {
		return principalToolCall(ctx, pluginToolResult, func(principal Principal) (GetPluginOutput, error) {
			return plugins.GetPlugin(ctx, principal, input)
		})
	})
}

func registerUnavailablePluginTools(reg *Registrar) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
		readOnly    bool
	}{
		{operationSetPluginAssignments, "Set Plugin Assignments", "Replace the complete assignment set of one exact plugin. This is not switched on for your organization yet.", false},
		{"list_plugin_assignments", "List Plugin Assignments", "List the roles and directory assignment targets that can receive plugins. This is not switched on for your organization yet.", true},
		{"list_plugins", "List Plugins", "List the plugins in a project. This is not switched on for your organization yet.", true},
		{"get_plugin", "Get One Plugin", "Get one plugin and what it carries. This is not switched on for your organization yet.", true},
	} {
		manifest := &mcp.Tool{Name: tool.name, Title: tool.title, Description: tool.description}
		if tool.readOnly {
			manifest.Annotations = readOnlyAnnotations()
		}
		addTool(reg, manifest, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, unavailableTool("plugins"))
	}
}

func pluginToolResult(err error) (*mcp.CallToolResult, bool) {
	var result pluginRefusalResult
	switch {
	case errors.Is(err, ErrPluginProjectNotFound):
		result = pluginRefusalResult{Code: "not_found", Message: "That project is not one you can use here. Pick one returned by list_projects."}
	case errors.Is(err, ErrPluginNotFound):
		result = pluginRefusalResult{Code: "not_found", Message: "No plugin in this project has that exact name. List the project's plugins with list_plugins and name one of them; nothing is picked by default."}
	case errors.Is(err, ErrPluginAmbiguous):
		result = pluginRefusalResult{Code: "ambiguous_target", Message: "More than one plugin in this project has that name. Name it by its ID instead."}
	case errors.Is(err, ErrPluginCursorInvalid):
		result = pluginRefusalResult{Code: "invalid_request", Message: "That page marker does not belong to this project. Start the list again from the beginning."}
	default:
		if mutation, ok := errors.AsType[*PluginAssignmentMutationError](err); ok {
			result = pluginRefusalResult{Code: mutation.Code, Message: mutation.Message}
			break
		}
		if budgetResult, ok := operationBudgetToolResult(err); ok {
			return budgetResult, true
		}
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
