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

// Plugin reads serve both audiences: the managed assistant resolves a plugin by
// name before distributing a skill to it, and this catalogue is the rollout
// replacement for the legacy toolset that carried platform_list_plugins, so an
// assistant reaches one of the two and never sees both names. The assistant
// adapter admits calls only from a live organization admin, so an assistant
// read always takes the administrative branch below.
//
// Plugin mutations stay externalOnly. Changing who receives a plugin is a
// confirmed, version-protected dashboard workflow that the assistant surface
// has not been reviewed to carry.
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
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListPluginAssignmentsInput) (*mcp.CallToolResult, ListPluginAssignmentsOutput, error) {
		return principalToolCall(ctx, pluginToolResult, func(principal Principal) (ListPluginAssignmentsOutput, error) {
			return plugins.ListPluginAssignments(ctx, principal, input)
		})
	})

	addTool(reg, &mcp.Tool{
		Name:        "list_plugins",
		Title:       "List Plugins",
		Description: "List plugins in an explicit project. Organization administrators see the administrative inventory. Other members see only published plugins currently assigned to their own user, role, directory audiences, email, or everyone; recipient counts and assignment details are withheld.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryOrgRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListPluginsInput) (*mcp.CallToolResult, ListPluginsOutput, error) {
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
		Description: "Get one exact plugin in an explicit project. Organization administrators see its administrative inventory, exact typed MCP membership IDs and targets, assignment controls, and publication evidence when configured. Admin membership results are cursor-paginated and complete beyond 100 entries, and a changed membership set requires restarting pagination. Publication evidence shows the addresses a package would contain now and compares its stored MCP fingerprints: fresh=true means matching stored inputs, fresh=false means different inputs, null means unknown. If unavailable=true, package inputs could not be resolved; do not treat an empty packages list as a package with no MCPs. This does not verify installed clients or live marketplace contents; publication=published alone does not mean addresses are current. Publication evidence also carries last_publish for the project's marketplace: last_recorded_publish_at (the last publish Gram successfully recorded; it can lag the marketplace), and the latest publish attempt's state (none, queued, running, retrying, succeeded, failed) with a failure_category and a fixed failure_message when it failed. Read it together with fresh: fresh=false with queued, running, or retrying means an update is on its way; fresh=false with failed means the update did not reach the marketplace, and the remedy is to republish, then Speakeasy support if it keeps failing, except repository_conflict, which needs Speakeasy support and does not clear on republish; fresh=false with succeeded or none means no publish has picked up the latest change yet. An administrator republishes with republish_plugin, which is offered only on external Platform MCP connections; a managed assistant, which cannot call it, should direct the administrator to the AICP dashboard instead. When publication evidence has not_configured=true, no publish has ever been recorded and republish_plugin refuses the project, so for a failed first publish send the administrator straight to the AICP dashboard or Speakeasy support rather than offering it. After republish_plugin returns enqueued, the request can be accepted before its run starts, so get_plugin may still describe the previous attempt: treat the request as pending until last_publish.requested_at is later than the republish, and do not read an older failed or succeeded attempt as the new request failing or being missed, or republish again on that basis. The marketplace repository is managed by Speakeasy, so there is no GitHub connection for the user to repair. If last_publish.unavailable=true, the attempt could not be read; do not infer a failure. Other members can read only a published plugin assigned to them, with its MCP servers and skills but no recipient identities, counts, assignment references, membership IDs, backend target IDs, package location, repository details, or credentials.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryOrgRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetPluginInput) (*mcp.CallToolResult, GetPluginOutput, error) {
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

	registerRepublishPluginTool(reg, plugins)

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
		// Audiences match the live registration so an assistant in an
		// organization without plugins sees a readable refusal rather than a
		// tool that appears only once the feature is switched on.
		audiences := externalOnly
		if tool.name == "list_plugins" || tool.name == "get_plugin" || tool.name == "list_plugin_assignments" {
			audiences = bothAudiences
		}
		addTool(reg, manifest, ToolMeta{Authorization: authority, Audiences: audiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryScopes}, unavailableTool("plugins"))
	}
	registerRepublishPluginTool(reg, nil)
}

const republishPluginDescription = "Request a publish of an explicit project's plugin packages now, for a plugin whose published package is stale (get_plugin reports publication_evidence.fresh=false), instead of waiting for the periodic refresh. " +
	"This regenerates the whole project's plugin marketplace: every plugin in the project is republished, not only the one named. Tell the user that, ask them to confirm, then call this with confirmed: true and a stable idempotency key. " +
	"Name the plugin exactly by ID, slug, or name; a name matching nothing is refused as not_found and one matching more than one plugin as ambiguous_target, with no fallback to the default plugin. " +
	"If the plugin's package already matches its current inputs, nothing is requested and the outcome is already_current. If the project has no package repository connected, this returns not_configured with a dashboard link. " +
	"outcome=enqueued means a publish was requested, not that it has landed: it runs in the background, so call get_plugin later and report fresh=true only once it says so. A retry with the same idempotency key is safe."

const unavailableRepublishPluginDescription = "Request a publish of a project's plugin packages now. This is not switched on for your organization yet; publish from the project's Plugins page in the AI Control Plane dashboard instead."

// republishPluginAnnotations are shared by the live and unavailable
// registrations. Republishing unchanged inputs is a no-op, so the tool is
// idempotent, and it never removes anything.
func republishPluginAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false)}
}

// registerRepublishPluginTool keeps one manifest for both registrations, so
// composing the publish path changes what the tool answers rather than whether
// it exists. Republishing stays external-only like the other plugin writes:
// it reaches every person holding any plugin in the project.
func registerRepublishPluginTool(reg *Registrar, plugins *PluginsService) {
	manifest := &mcp.Tool{
		Name:        operationRepublishPlugin,
		Title:       "Republish a Project's Plugins",
		Description: republishPluginDescription,
		Annotations: republishPluginAnnotations(),
	}
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}
	if !plugins.republishValid() {
		manifest.Description = unavailableRepublishPluginDescription
		addTool(reg, manifest, meta, func(context.Context, *mcp.CallToolRequest, RepublishPluginInput) (*mcp.CallToolResult, RepublishPluginOutput, error) {
			refusal, _ := republishPluginToolResult(pluginRepublishUnavailable(nil))
			return refusal, RepublishPluginOutput{}, nil
		})
		return
	}
	addTool(reg, manifest, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input RepublishPluginInput) (*mcp.CallToolResult, RepublishPluginOutput, error) {
		return principalToolCall(ctx, republishPluginToolResult, func(principal Principal) (RepublishPluginOutput, error) {
			return plugins.RepublishPlugin(ctx, principal, input)
		})
	})
}

type pluginRepublishRefusalResult struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	DashboardURL string `json:"dashboard_url,omitempty"`
}

func republishPluginToolResult(err error) (*mcp.CallToolResult, bool) {
	republish, ok := errors.AsType[*PluginRepublishError](err)
	if !ok {
		return pluginToolResult(err)
	}
	content, marshalErr := json.Marshal(pluginRepublishRefusalResult{Code: republish.Code, Message: republish.Message, DashboardURL: republish.DashboardURL})
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
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
