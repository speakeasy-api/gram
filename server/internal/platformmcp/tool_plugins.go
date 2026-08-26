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
	addTool(reg, &mcp.Tool{
		Name:        "list_plugins",
		Title:       "List Plugins",
		Description: "List the plugins in a named project. A plugin is the bundle of MCP servers and skills an administrator shares with people, so this is the level to answer \"what do we ship\" at, rather than adding up individual servers. Each entry reports how much the plugin carries, who receives it, and whether it has been published — that is, whether the people it is shared with have it yet.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListPluginsInput) (*mcp.CallToolResult, ListPluginsOutput, error) {
		return pluginToolCall(ctx, func(principal Principal) (ListPluginsOutput, error) {
			return plugins.ListPlugins(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_plugin",
		Title:       "Get One Plugin",
		Description: "Get one plugin — the bundle of MCP servers and skills you share with people — and what it carries: its MCP servers, and its skills with the version each is fixed to. Constraints: name the plugin exactly by ID, slug, or name; a name matching nothing is refused as not_found and a name matching more than one plugin as ambiguous_target, never silently answered with the default plugin.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetPluginInput) (*mcp.CallToolResult, GetPluginOutput, error) {
		return pluginToolCall(ctx, func(principal Principal) (GetPluginOutput, error) {
			return plugins.GetPlugin(ctx, principal, input)
		})
	})
}

func registerUnavailablePluginTools(reg *Registrar) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
	}{
		{"list_plugins", "List Plugins", "List the plugins in a project. This is not switched on for your organization yet."},
		{"get_plugin", "Get One Plugin", "Get one plugin and what it carries. This is not switched on for your organization yet."},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, unavailableTool("plugins"))
	}
}

// pluginToolCall runs one plugin call and turns a refusal into a structured
// error result rather than a transport error, so the reason survives to the
// model that has to act on it.
func pluginToolCall[Out any](ctx context.Context, call func(principal Principal) (Out, error)) (*mcp.CallToolResult, Out, error) {
	var zero Out
	principal, err := principalFromToolContext(ctx)
	if err != nil {
		return nil, zero, err
	}
	output, err := call(principal)
	if err != nil {
		if refusal, ok := pluginToolResult(err); ok {
			return refusal, zero, nil
		}
		return nil, zero, err
	}
	return nil, output, nil
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
