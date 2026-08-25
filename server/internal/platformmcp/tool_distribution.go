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
	ProjectSlug string `json:"project_slug" jsonschema:"explicit AICP project slug selected by the user"`
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
		Title:       "Distribute MCP to Plugin",
		Description: "Add the workflow-bound, ready MCP Catalogue server to one exact existing plugin in the explicit project. The project must already be selected, registered, and freshly ready through the dashboard setup flow. Name the plugin exactly by ID, slug, or name — a name matching nothing is refused as not_found and a name matching more than one plugin as ambiguous_target, and neither falls back to the default plugin. This never creates a plugin.",
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
			Message:          "The MCP Catalogue server is in the " + distribution.Plugin + " plugin. Install or refresh that project's package, then call one selected-project MCP tool to verify it works.",
		}, nil
	})

	addTool(reg, &mcp.Tool{
		Name:        "remove_mcp_from_plugin",
		Title:       "Remove MCP from Plugin",
		Description: "Remove the workflow-bound MCP Catalogue server from one exact existing plugin in the explicit project, reversing distribute_mcp_to_plugin. Only an attachment this workflow created is removed; an attachment an administrator made stays. Name the plugin exactly, on the same terms as distribute_mcp_to_plugin.",
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
			Message:          "The MCP Catalogue server is no longer carried by the " + distribution.Plugin + " plugin through this workflow. Refresh that project's package for the change to reach installed clients.",
		}, nil
	})
}

func distributionToolError(err error) (*mcp.CallToolResult, bool) {
	var result distributionErrorResult
	switch {
	case errors.Is(err, ErrDistributionNotReady):
		result = distributionErrorResult{Code: "not_ready", Message: "Complete secure setup and recheck fresh readiness in the AI Control Plane dashboard before adding this MCP."}
	case errors.Is(err, ErrPluginNotFound):
		result = distributionErrorResult{Code: "not_found", Message: "No plugin in this project matches that target exactly. List the project's plugins with list_plugins and name one of them; there is no fallback to the default plugin."}
	case errors.Is(err, ErrPluginAmbiguous):
		result = distributionErrorResult{Code: "ambiguous_target", Message: "More than one plugin in this project matches that name. Name the plugin by its ID instead."}
	case errors.Is(err, ErrDistributionDefaultAbsent):
		result = distributionErrorResult{Code: "default_plugin_missing", Message: "This project does not have an existing Default plugin, so Platform MCP cannot add the MCP."}
	case errors.Is(err, ErrDistributionInvalid):
		result = distributionErrorResult{Code: "no_distribution_target", Message: "No registered MCP is bound to this session for that project. Give the exact slug of the project you registered in, and register the MCP with register_catalog_mcp or register_remote_mcp there, before distributing."}
	case errors.Is(err, ErrDistributionTargetUnavailable):
		result = distributionErrorResult{Code: "distribution_target_unavailable", Message: "The selected project or its Platform MCP setup is no longer available. Check onboarding status and choose the supported next action."}
	case errors.Is(err, ErrDistributionConflict):
		result = distributionErrorResult{Code: "conflict", Message: "The project distribution changed. Check onboarding status and retry the supported next action."}
	case errors.Is(err, ErrDistributionBlockedPendingApproval):
		result = distributionErrorResult{Code: "conflict", Message: "This MCP is awaiting Shadow MCP approval, so it cannot be distributed yet. Ask an administrator to approve it in the AI Control Plane dashboard, then retry."}
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
