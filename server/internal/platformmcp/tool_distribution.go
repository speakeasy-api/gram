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
	Plugin      string `json:"plugin" jsonschema:"exact existing plugin in the project, by ID, slug, or name, as returned by list_plugins; there is no implicit default"`
}

type DistributeMCPToolOutput struct {
	ProjectSlug      string `json:"project_slug"`
	Plugin           string `json:"plugin"`
	Attached         bool   `json:"attached"`
	PublicationState string `json:"publication_state"`
	Message          string `json:"message"`
}

type distributionErrorResult struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func registerDistributionTools(reg *Registrar, onboarding *OnboardingService, distributions *DistributionService) {
	addTool(reg, &mcp.Tool{
		Name:        "distribute_mcp_to_plugin",
		Title:       "Add an MCP Server to a Plugin",
		Description: "Give a working MCP server to one plugin — the bundle of MCP servers and skills you share with people — so everyone it is shared with gets it. Constraints: the project must already be selected, the MCP server registered, and freshly confirmed working through the dashboard setup flow. Name the plugin exactly by ID, slug, or name: a name matching nothing is refused as not_found and a name matching more than one plugin as ambiguous_target, and neither falls back to the default plugin. This never creates a plugin.",
	}, ToolMeta{
		// External-only: distribution rows require a connection, which a
		// connection-less surface cannot satisfy.
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DistributeMCPToolInput) (*mcp.CallToolResult, DistributeMCPToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, DistributeMCPToolOutput{}, err
		}
		if input.ProjectSlug == "" {
			result, _ := distributionToolError(ErrDistributionInvalid)
			return result, DistributeMCPToolOutput{}, nil
		}
		// A blank plugin is refused rather than resolved: on this surface the
		// caller is an agent acting on a name a person gave it, and silently
		// sending an MCP to the default plugin is the failure this tool exists
		// to prevent.
		if strings.TrimSpace(input.Plugin) == "" {
			result, _ := distributionToolError(ErrPluginNotFound)
			return result, DistributeMCPToolOutput{}, nil
		}
		distribution, err := distributions.DistributeForOnboarding(ctx, principal, input.ProjectSlug, input.Plugin)
		if err != nil {
			if result, ok := distributionToolError(err); ok {
				return result, DistributeMCPToolOutput{}, nil
			}
			return nil, DistributeMCPToolOutput{}, err
		}
		if distribution.AttachmentLive {
			projection, err := onboarding.Get(ctx, principal.OrganizationID, principal.UserID)
			if err != nil {
				return nil, DistributeMCPToolOutput{}, err
			}
			if projection.Workflow == nil || projection.SelectedProject == nil || projection.SelectedProject.Slug != input.ProjectSlug || projection.Workflow.SelectedRegistrationID == uuid.Nil {
				return nil, DistributeMCPToolOutput{}, ErrUnavailable
			}
			if err := onboarding.RecordDistributionSucceeded(ctx, principal, projection.SelectedProject.ID, projection.Workflow.SelectedRegistrationID); err != nil {
				return nil, DistributeMCPToolOutput{}, err
			}
		}
		return nil, DistributeMCPToolOutput{
			ProjectSlug:      input.ProjectSlug,
			Plugin:           distribution.Plugin,
			Attached:         distribution.AttachmentLive,
			PublicationState: distribution.PublicationState,
			Message:          distributionOutcomeMessage(distribution.Plugin, distribution.PublicationState, false),
		}, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "remove_mcp_from_plugin",
		Title:       "Remove an MCP Server from a Plugin",
		Description: "Take an MCP server back out of one plugin, undoing distribute_mcp_to_plugin, so the people it is shared with stop getting it. Constraints: only a membership this flow created is removed; one an administrator made by hand stays. Name the plugin exactly, on the same terms as distribute_mcp_to_plugin.",
	}, ToolMeta{
		// External-only for the same reason as distribution: the removal is
		// recorded against the caller's connection.
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DistributeMCPToolInput) (*mcp.CallToolResult, DistributeMCPToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, DistributeMCPToolOutput{}, err
		}
		if input.ProjectSlug == "" {
			result, _ := distributionToolError(ErrDistributionInvalid)
			return result, DistributeMCPToolOutput{}, nil
		}
		if strings.TrimSpace(input.Plugin) == "" {
			result, _ := distributionToolError(ErrPluginNotFound)
			return result, DistributeMCPToolOutput{}, nil
		}
		current, err := distributions.Current(ctx, principal, input.ProjectSlug, input.Plugin)
		if err != nil {
			if result, ok := distributionToolError(err); ok {
				return result, DistributeMCPToolOutput{}, nil
			}
			return nil, DistributeMCPToolOutput{}, err
		}
		distribution, err := distributions.Remove(ctx, principal, DistributionInput{ProjectSlug: input.ProjectSlug, Plugin: input.Plugin, ExpectedVersion: current.Version})
		if err != nil {
			if result, ok := distributionToolError(err); ok {
				return result, DistributeMCPToolOutput{}, nil
			}
			return nil, DistributeMCPToolOutput{}, err
		}
		return nil, DistributeMCPToolOutput{
			ProjectSlug:      input.ProjectSlug,
			Plugin:           distribution.Plugin,
			Attached:         distribution.AttachmentLive,
			PublicationState: distribution.PublicationState,
			Message:          distributionOutcomeMessage(distribution.Plugin, distribution.PublicationState, true),
		}, nil
	})
}

func distributionToolError(err error) (*mcp.CallToolResult, bool) {
	var result distributionErrorResult
	switch {
	case errors.Is(err, ErrDistributionNotReady):
		result = distributionErrorResult{Code: "not_ready", Message: "This MCP server has not been confirmed working yet, so it cannot be shared with anyone. Finish setting it up in the dashboard — its source and its sign-in — then check it again."}
	case errors.Is(err, ErrPluginNotFound):
		result = distributionErrorResult{Code: "not_found", Message: "No plugin in this project has that exact name. List the project's plugins with list_plugins and name one of them; nothing is picked by default."}
	case errors.Is(err, ErrPluginAmbiguous):
		result = distributionErrorResult{Code: "ambiguous_target", Message: "More than one plugin in this project has that name. Name it by its ID instead."}
	case errors.Is(err, ErrDistributionDefaultAbsent):
		result = distributionErrorResult{Code: "default_plugin_missing", Message: "This project has no Default plugin, so the MCP server cannot be added there. Name an existing plugin instead."}
	case errors.Is(err, ErrDistributionInvalid):
		result = distributionErrorResult{Code: "no_distribution_target", Message: "No MCP server from this session has been added to that project. Give the exact slug of the project you added it to, and add the MCP server there with register_catalog_mcp or register_remote_mcp first."}
	case errors.Is(err, ErrDistributionTargetUnavailable):
		result = distributionErrorResult{Code: "distribution_target_unavailable", Message: "That project, or this MCP server's setup in it, is no longer available. Check where setup got to and take the next step it offers."}
	case errors.Is(err, ErrDistributionConflict):
		result = distributionErrorResult{Code: "conflict", Message: "What this project shares has changed since you last looked. Check where setup got to and try the next step it offers."}
	case errors.Is(err, ErrDistributionBlockedPendingApproval):
		result = distributionErrorResult{Code: "conflict", Message: "This MCP server is waiting for an administrator to approve it, so it cannot be shared yet. Ask an administrator to approve it in the dashboard, then try again."}
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}

// distributionOutcomeMessage says what actually reached people, not merely what
// was recorded. Membership can commit while publishing the package fails, which
// leaves the change real in the project and absent from every installed client.
// The server instructions tell the model not to narrate publication_state on its
// own, so this message is the only account the administrator hears — claiming
// delivery unconditionally would make a failed publish silent.
func distributionOutcomeMessage(plugin, publicationState string, removed bool) string {
	membership := "This MCP server is now part of the " + plugin + " plugin"
	if removed {
		membership = "This MCP server is no longer part of the " + plugin + " plugin"
	}
	switch publicationState {
	case publicationStateCurrent:
		if removed {
			return membership + ", and the people it was shared with will stop getting it. Refresh that project's package for the change to reach them."
		}
		return membership + ", and the people it is shared with will get it. Install or refresh that project's package, then try one of its tools to confirm it works."
	case publicationStatePending:
		return membership + ". The package that carries that change to people has not finished updating yet, so check again shortly."
	default:
		if removed {
			return membership + ", but the package that carries that change to people could not be updated, so they may still have it. Publish it again from the dashboard."
		}
		return membership + ", but the package that carries it to people could not be updated, so they do not have it yet. Publish it again from the dashboard."
	}
}
