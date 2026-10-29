//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type DistributeMCPToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit project slug selected by the user"`
	MCPID       string `json:"mcp_id" jsonschema:"exact MCP server ID in that project, as returned by find_mcp or get_mcp; there is no implicit default"`
	Plugin      string `json:"plugin" jsonschema:"exact existing plugin in the project, by ID, slug, or name, as returned by list_plugins; there is no implicit default"`
}

type DistributeMCPToolOutput struct {
	ProjectSlug      string             `json:"project_slug"`
	MCPID            string             `json:"mcp_id"`
	MCPServer        string             `json:"mcp_server"`
	Plugin           string             `json:"plugin"`
	Attached         bool               `json:"attached"`
	PublicationState string             `json:"publication_state"`
	ShadowMCP        *ShadowMCPStanding `json:"shadow_mcp,omitempty"`
	Message          string             `json:"message"`
}

type distributionErrorResult struct {
	Code      string `json:"code"`
	MCPServer string `json:"mcp_server,omitempty"`
	Message   string `json:"message"`
}

func registerDistributionTools(reg *Registrar, onboarding *OnboardingService, distributions *DistributionService, registrations *RegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "distribute_mcp_to_plugin",
		Title:       "Add an MCP Server to a Plugin",
		Description: "Give a working MCP server to one plugin — the bundle of MCP servers and skills you share with people — so everyone it is shared with gets it. Constraints: the project must already be selected and the MCP server added to it through setup. Name the server exactly by its ID from find_mcp or get_mcp; an ID matching no such server in the project is refused as mcp_not_found. That server's readiness is checked when this is called, from a current stored check or, if there is none, one new check; a server that is not working is refused as not_ready, and the result names the server either way. Name the plugin exactly by ID, slug, or name: a name matching nothing is refused as not_found and a name matching more than one plugin as ambiguous_target, and neither falls back to the default plugin. This never creates a plugin. For a server added by URL, the administrator's own distribution counts as the Shadow MCP review when the project has a policy that blocks unreviewed servers: the server is recorded in Shadow MCP as approved for this plugin's audience, and shadow_mcp in the result says so with the page where that standing is seen and changed.",
		Annotations: distributeMCPToPluginAnnotations(),
	}, ToolMeta{
		Authorization: ExternalAuthorizationOrgAdmin,
		// External-only: distribution rows require a connection, which a
		// connection-less surface cannot satisfy.
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DistributeMCPToolInput) (*mcp.CallToolResult, DistributeMCPToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, DistributeMCPToolOutput{}, err
		}
		if input.ProjectSlug == "" {
			result, _ := distributionToolError(ErrDistributionInvalid, "")
			return result, DistributeMCPToolOutput{}, nil
		}
		// A blank plugin is refused rather than resolved: on this surface the
		// caller is an agent acting on a name a person gave it, and silently
		// sending an MCP to the default plugin is the failure this tool exists
		// to prevent.
		if strings.TrimSpace(input.Plugin) == "" {
			result, _ := distributionToolError(ErrPluginNotFound, "")
			return result, DistributeMCPToolOutput{}, nil
		}
		// A blank or malformed server is refused for the same reason: the
		// server the session last selected need not be the one the person named.
		mcpServerID, err := uuid.Parse(strings.TrimSpace(input.MCPID))
		if err != nil || mcpServerID == uuid.Nil {
			result, _ := distributionToolError(ErrDistributionMCPNotFound, "")
			return result, DistributeMCPToolOutput{}, nil
		}
		target, err := distributions.Target(ctx, principal, input.ProjectSlug, mcpServerID)
		if err != nil {
			if result, ok := distributionToolError(err, ""); ok {
				return result, DistributeMCPToolOutput{}, nil
			}
			return nil, DistributeMCPToolOutput{}, err
		}
		// Check the named server now, so a server confirmed working in an
		// earlier step or chat is not refused as unchecked, and one that has
		// stopped working is not shared.
		if registrations != nil && registrations.readiness != nil {
			_, readiness, found, err := registrations.readiness.GetReadiness(ctx, principal, target.ProjectSlug, target.RegistrationID.String(), false)
			if err != nil {
				if result, ok := operationBudgetToolResult(err); ok {
					return result, DistributeMCPToolOutput{}, nil
				}
				return nil, DistributeMCPToolOutput{}, err
			}
			if !found || readiness.State != ReadinessReady || !readiness.Fresh {
				result, _ := distributionToolError(ErrDistributionNotReady, target.MCPServerName)
				return result, DistributeMCPToolOutput{}, nil
			}
		}
		distribution, err := distributions.DistributeForOnboarding(ctx, principal, input.ProjectSlug, mcpServerID, input.Plugin)
		if err != nil {
			if result, ok := distributionToolError(err, target.MCPServerName); ok {
				return result, DistributeMCPToolOutput{}, nil
			}
			return nil, DistributeMCPToolOutput{}, err
		}
		if distribution.AttachmentLive {
			// The onboarding milestone belongs to the server the workflow
			// selected; sharing any other server leaves it untouched.
			projection, err := onboarding.Get(ctx, principal.OrganizationID, principal.UserID)
			if err != nil {
				return nil, DistributeMCPToolOutput{}, err
			}
			if projection.Workflow != nil && projection.SelectedProject != nil && projection.SelectedProject.Slug == input.ProjectSlug && projection.Workflow.SelectedRegistrationID == target.RegistrationID {
				if err := onboarding.RecordDistributionSucceeded(ctx, principal, projection.SelectedProject.ID, target.RegistrationID); err != nil {
					return nil, DistributeMCPToolOutput{}, err
				}
			}
		}
		return nil, DistributeMCPToolOutput{
			ProjectSlug:      input.ProjectSlug,
			MCPID:            mcpServerID.String(),
			MCPServer:        distribution.MCPServer,
			Plugin:           distribution.Plugin,
			Attached:         distribution.AttachmentLive,
			PublicationState: distribution.PublicationState,
			ShadowMCP:        distribution.ShadowMCP,
			Message:          distributionOutcomeMessage(distribution.MCPServer, distribution.Plugin, distribution.PublicationState, distribution.AttachmentLive, false) + shadowMCPStandingMessage(distribution.ShadowMCP),
		}, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "remove_mcp_from_plugin",
		Title:       "Remove an MCP Server from a Plugin",
		Description: "Take an MCP server back out of one plugin, undoing distribute_mcp_to_plugin, so the people it is shared with stop getting it. Constraints: only a membership this flow created is removed; one an administrator made by hand stays. Name the server and the plugin exactly, on the same terms as distribute_mcp_to_plugin. Removing it from a plugin does not revoke any Shadow MCP approval recorded for it; adding it back to a plugin whose audience that approval covers is admitted without a new approval. Narrow or revoke the approval itself with decide_shadow_mcp_access, or from the server's Shadow MCP page.",
		Annotations: removeMCPFromPluginAnnotations(),
	}, ToolMeta{
		Authorization: ExternalAuthorizationOrgAdmin,
		// External-only for the same reason as distribution: the removal is
		// recorded against the caller's connection.
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DistributeMCPToolInput) (*mcp.CallToolResult, DistributeMCPToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, DistributeMCPToolOutput{}, err
		}
		if input.ProjectSlug == "" {
			result, _ := distributionToolError(ErrDistributionInvalid, "")
			return result, DistributeMCPToolOutput{}, nil
		}
		if strings.TrimSpace(input.Plugin) == "" {
			result, _ := distributionToolError(ErrPluginNotFound, "")
			return result, DistributeMCPToolOutput{}, nil
		}
		mcpServerID, err := uuid.Parse(strings.TrimSpace(input.MCPID))
		if err != nil || mcpServerID == uuid.Nil {
			result, _ := distributionToolError(ErrDistributionMCPNotFound, "")
			return result, DistributeMCPToolOutput{}, nil
		}
		// Resolve the server first so a plugin refusal still names it.
		target, err := distributions.Target(ctx, principal, input.ProjectSlug, mcpServerID)
		if err != nil {
			if result, ok := distributionToolError(err, ""); ok {
				return result, DistributeMCPToolOutput{}, nil
			}
			return nil, DistributeMCPToolOutput{}, err
		}
		current, err := distributions.Current(ctx, principal, input.ProjectSlug, mcpServerID, input.Plugin)
		if err != nil {
			if result, ok := distributionToolError(err, target.MCPServerName); ok {
				return result, DistributeMCPToolOutput{}, nil
			}
			return nil, DistributeMCPToolOutput{}, err
		}
		distribution, err := distributions.Remove(ctx, principal, DistributionInput{ProjectSlug: input.ProjectSlug, MCPServerID: mcpServerID, Plugin: input.Plugin, ExpectedVersion: current.Version})
		if err != nil {
			if result, ok := distributionToolError(err, target.MCPServerName); ok {
				return result, DistributeMCPToolOutput{}, nil
			}
			return nil, DistributeMCPToolOutput{}, err
		}
		return nil, DistributeMCPToolOutput{
			ProjectSlug:      input.ProjectSlug,
			MCPID:            mcpServerID.String(),
			MCPServer:        distribution.MCPServer,
			Plugin:           distribution.Plugin,
			Attached:         distribution.AttachmentLive,
			PublicationState: distribution.PublicationState,
			ShadowMCP:        nil,
			Message:          distributionOutcomeMessage(distribution.MCPServer, distribution.Plugin, distribution.PublicationState, distribution.AttachmentLive, true),
		}, nil
	})
}

// distributionToolError maps a distribution refusal to its tool result. server
// is the name of the MCP server the call acted on, when known, so a refusal
// says which server it is about.
func distributionToolError(err error, server string) (*mcp.CallToolResult, bool) {
	var result distributionErrorResult
	switch {
	case errors.Is(err, ErrDistributionMCPNotFound):
		result = distributionErrorResult{Code: "mcp_not_found", Message: "No MCP server added through setup in that project has that ID. Find the server with find_mcp in that project and name it by its ID; nothing is picked by default."}
	case errors.Is(err, ErrDistributionNotReady):
		result = distributionErrorResult{Code: "not_ready", Message: mcpServerSubject(server) + " has not been confirmed working, so it cannot be shared with anyone. Check it with get_mcp_readiness; if that says it is not working, finish setting it up in the dashboard — its source and its sign-in — then check it again."}
	case errors.Is(err, ErrPluginNotFound):
		result = distributionErrorResult{Code: "not_found", Message: "No plugin in this project has that exact name. List the project's plugins with list_plugins and name one of them; nothing is picked by default."}
	case errors.Is(err, ErrPluginAmbiguous):
		result = distributionErrorResult{Code: "ambiguous_target", Message: "More than one plugin in this project has that name. Name it by its ID instead."}
	case errors.Is(err, ErrDistributionDefaultAbsent):
		result = distributionErrorResult{Code: "default_plugin_missing", Message: "This project has no Default plugin, so the MCP server cannot be added there. Name an existing plugin instead."}
	case errors.Is(err, ErrDistributionInvalid):
		result = distributionErrorResult{Code: "no_distribution_target", Message: "There is no MCP server added through setup in that project to act on. Give the exact slug of the project the server was added to and the server's ID from find_mcp in that project; do not add the server again."}
	case errors.Is(err, ErrDistributionTargetUnavailable):
		result = distributionErrorResult{Code: "distribution_target_unavailable", Message: "That project, or this MCP server's setup in it, is no longer available. Check where setup got to and take the next step it offers."}
	case errors.Is(err, ErrDistributionConflict):
		result = distributionErrorResult{Code: "conflict", Message: "What this project shares has changed since you last looked. Check where setup got to and try the next step it offers."}
	case errors.Is(err, ErrDistributionBlockedPendingApproval):
		result = distributionErrorResult{Code: "approval_required", Message: "This organisation's Shadow MCP policy blocks this server for the people it would reach, and this call could not be recorded as its approval. Review the server in the dashboard, then try again."}
	case errors.Is(err, ErrDistributionAdmissionUnavailable):
		result = distributionErrorResult{Code: "distribution_unavailable", Message: "Distribution approval could not be verified safely. No sharing change was made."}
	default:
		return nil, false
	}
	result.MCPServer = server
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}

// distributionOutcomeMessage says what actually reached people, not merely what
// was recorded. Two things can diverge from the membership write: publishing the
// package can fail independently, leaving the change real in the project and
// absent from every installed client; and a removal deliberately preserves an
// attachment an administrator made by hand, so the MCP server keeps reaching
// people even though this flow's own membership is gone. The server instructions
// tell the model not to narrate publication_state on its own, so this message is
// the only account the administrator hears.
func distributionOutcomeMessage(server, plugin, publicationState string, attachmentLive, removed bool) string {
	subject := mcpServerSubject(server)
	if removed {
		if attachmentLive {
			// Remove only withdraws the attachment this flow created; see
			// DistributionService.Remove, which returns the attachment authority
			// after publication. Saying people will stop receiving it here would
			// be false.
			return subject + " is no longer part of the " + plugin + " plugin through this setup, but an administrator added it to that plugin separately and that stays, so the people it is shared with still get it. Remove that one in the dashboard to stop it reaching them."
		}
		switch publicationState {
		case publicationStateCurrent:
			return subject + " is no longer part of the " + plugin + " plugin, and the people it was shared with will stop getting it. Refresh that project's package for the change to reach them."
		case publicationStatePending:
			return subject + " is no longer part of the " + plugin + " plugin. The package that carries that change to people has not finished updating yet, so check again shortly."
		default:
			return subject + " is no longer part of the " + plugin + " plugin, but the package that carries that change to people could not be updated, so the change may not have reached them yet. Publish it again from the dashboard."
		}
	}
	switch publicationState {
	case publicationStateCurrent:
		return subject + " is now part of the " + plugin + " plugin, and the people it is shared with will get it. Install or refresh that project's package, then try one of its tools to confirm it works."
	case publicationStatePending:
		return subject + " is now part of the " + plugin + " plugin. The package that carries it to people has not finished updating yet, so check again shortly."
	default:
		// This may be a first distribution or a later update to one already out
		// there, and repair_required does not distinguish them, so the copy says
		// the change has not landed rather than that nobody has the MCP server.
		return subject + " is now part of the " + plugin + " plugin, but the package that carries it to people could not be updated, so the change may not have reached them yet. Publish it again from the dashboard."
	}
}

// shadowMCPStandingMessage tells the administrator what their distribution
// meant for the project's Shadow MCP policy, in the words the policy page uses.
func shadowMCPStandingMessage(standing *ShadowMCPStanding) string {
	if standing == nil {
		return ""
	}
	switch standing.State {
	case shadowMCPStandingApproved:
		// Not attributed to this write: an earlier decision may already have
		// covered the audience, in which case nothing new was recorded.
		return " This project has a Shadow MCP policy, and the server is approved under it for the people this plugin reaches; an administrator's distribution is what records that approval when none covers them yet. Narrow or revoke it by deciding the server's Shadow MCP access, or from its Shadow MCP page."
	case DistributionAdmissionNotApplicable, "not_required":
		return ""
	case DistributionAdmissionEmptyAudience:
		return " No one is assigned to this plugin yet, so the project's Shadow MCP policy has nothing to approve until people are."
	default:
		return " Its standing with the project's Shadow MCP policy could not be confirmed; check its Shadow MCP page."
	}
}

// mcpServerSubject names the MCP server a message is about, falling back to a
// generic subject for a server with no recorded name.
func mcpServerSubject(server string) string {
	if server == "" {
		return "This MCP server"
	}
	return "The MCP server \"" + server + "\""
}
