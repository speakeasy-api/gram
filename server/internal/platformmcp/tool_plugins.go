//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/authz"
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
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input SetPluginAssignmentsInput) (*mcp.CallToolResult, SetPluginAssignmentsOutput, error) {
		return principalToolCall(ctx, pluginToolResult, func(principal Principal) (SetPluginAssignmentsOutput, error) {
			return plugins.SetPluginAssignments(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_plugin_assignments",
		Title:       "List Plugin Assignments",
		Description: "List up to 100 existing roles and directory assignment targets that can receive plugins in an explicit project. Each assignment has a short-lived opaque reference and, where available, a privacy-safe member count; Everyone has no member count. Raw principal identifiers are never returned. If truncated is true, use the dashboard to choose from the complete assignment set.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListPluginAssignmentsInput) (*mcp.CallToolResult, ListPluginAssignmentsOutput, error) {
		return principalToolCall(ctx, pluginToolResult, func(principal Principal) (ListPluginAssignmentsOutput, error) {
			return plugins.ListPluginAssignments(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_plugins",
		Title:       "List Plugins",
		Description: "List plugins in an explicit project. Organization administrators see the administrative inventory. Other members see only published plugins currently assigned to their own user, role, directory audiences, email, or everyone; recipient counts and assignment details are withheld.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryOrgRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListPluginsInput) (*mcp.CallToolResult, ListPluginsOutput, error) {
		return principalToolCall(ctx, pluginToolResult, func(principal Principal) (ListPluginsOutput, error) {
			admin, err := plugins.IsOrganizationAdmin(ctx, principal)
			if err != nil {
				return ListPluginsOutput{}, err
			}
			if admin {
				return plugins.ListPlugins(ctx, principal, input)
			}
			return plugins.ListAssignedPlugins(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_plugin",
		Title:       "Get One Plugin",
		Description: "Get one exact plugin in an explicit project. Organization administrators see its administrative inventory and assignment controls. Other members can read only a published plugin assigned to them, with its MCP servers and skills but no recipient identities, counts, assignment references, package location, repository details, or credentials.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryOrgRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetPluginInput) (*mcp.CallToolResult, GetPluginOutput, error) {
		return principalToolCall(ctx, pluginToolResult, func(principal Principal) (GetPluginOutput, error) {
			admin, err := plugins.IsOrganizationAdmin(ctx, principal)
			if err != nil {
				return GetPluginOutput{}, err
			}
			if admin {
				return plugins.GetPlugin(ctx, principal, input)
			}
			return plugins.GetAssignedPlugin(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_my_install_instructions",
		Title:       "Get My Install Instructions",
		Description: "Get non-secret, client-specific installation guidance for one assigned published plugin or one configured MCP server the caller may connect to. Name exactly one target in an explicit project. Plugin guidance rechecks the caller's current recipient assignments; standalone MCP guidance rechecks mcp:connect. No marketplace token, repository credential, package bytes, API key, or OAuth credential is returned.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryOrgReadOrMCPConnect}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMyInstallInstructionsInput) (*mcp.CallToolResult, GetMyInstallInstructionsOutput, error) {
		return principalToolCall(ctx, installInstructionToolResult, func(principal Principal) (GetMyInstallInstructionsOutput, error) {
			return plugins.GetMyInstallInstructions(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_my_mcp_access",
		Title:       "Check My MCP Access",
		Description: "Check only your current read and connection access to one exact MCP server. This evaluates live RBAC without creating an access challenge and never returns roles, grants, other members, or hidden targets. If connection access is missing, it returns the exact required scope and a safe request-access link when available.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryMCPReadOrConnect}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMyMCPStatusInput) (*mcp.CallToolResult, GetMyMCPAccessOutput, error) {
		return principalToolCall(ctx, memberMCPStatusToolResult, func(principal Principal) (GetMyMCPAccessOutput, error) {
			return plugins.GetMyMCPAccess(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "get_my_mcp_connection_status",
		Title:       "Check My MCP Connection",
		Description: "Check only your current authorization state for one exact MCP server. Returns a bounded state and the canonical MCP endpoint for reconnecting through the existing client-owned OAuth flow. Never returns tokens, OAuth codes, secrets, account identities, upstream-granted scopes, raw sessions, provider diagnostics, or other users' connections.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryMCPReadOrConnect}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMyMCPStatusInput) (*mcp.CallToolResult, GetMyMCPConnectionStatusOutput, error) {
		return principalToolCall(ctx, memberMCPStatusToolResult, func(principal Principal) (GetMyMCPConnectionStatusOutput, error) {
			return plugins.GetMyMCPConnectionStatus(ctx, principal, input)
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
		{"get_my_install_instructions", "Get My Install Instructions", "Get non-secret install guidance for an assigned plugin or permitted MCP server. This is not switched on for your organization yet.", true},
		{"get_my_mcp_access", "Check My MCP Access", "Check your access to one MCP server. This is not switched on for your organization yet.", true},
		{"get_my_mcp_connection_status", "Check My MCP Connection", "Check your authorization state for one MCP server. This is not switched on for your organization yet.", true},
	} {
		manifest := &mcp.Tool{Name: tool.name, Title: tool.title, Description: tool.description}
		if tool.readOnly {
			manifest.Annotations = readOnlyAnnotations()
		}
		authority := ExternalAuthorizationOrgAdmin
		if tool.name == "list_plugins" || tool.name == "get_plugin" || tool.name == "get_my_install_instructions" || tool.name == "get_my_mcp_access" || tool.name == "get_my_mcp_connection_status" {
			authority = ExternalAuthorizationMember
		}
		var discoveryScopes []authz.Scope
		switch tool.name {
		case "list_plugins", "get_plugin":
			discoveryScopes = discoveryOrgRead
		case "get_my_install_instructions":
			discoveryScopes = discoveryOrgReadOrMCPConnect
		case "get_my_mcp_access", "get_my_mcp_connection_status":
			discoveryScopes = discoveryMCPReadOrConnect
		}
		addTool(reg, manifest, ToolMeta{Authorization: authority, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryScopes}, unavailableTool("plugins"))
	}
}

func memberMCPStatusToolResult(err error) (*mcp.CallToolResult, bool) {
	if errors.Is(err, ErrMemberMCPStatusTargetNotFound) {
		content, marshalErr := json.Marshal(pluginRefusalResult{
			Code: "not_found", Message: "No MCP server matching that exact identifier is available to you in this project. Choose one returned by find_mcp or get_plugin.",
		})
		if marshalErr != nil {
			return nil, false
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
	}
	return pluginToolResult(err)
}

func installInstructionToolResult(err error) (*mcp.CallToolResult, bool) {
	if errors.Is(err, ErrInstallTargetNotFound) {
		content, marshalErr := json.Marshal(pluginRefusalResult{
			Code: "not_found", Message: "No installable target matching that exact identifier is available in this project. List the resources you can access and choose one of those targets.",
		})
		if marshalErr != nil {
			return nil, false
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
	}
	return pluginToolResult(err)
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
