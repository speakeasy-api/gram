//nolint:exhaustruct,wrapcheck // MCP SDK manifests use optional zero values and preserve bounded typed errors.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RegisterOnboardingMCPToolInput struct {
	ProjectSlug     string                     `json:"project_slug" jsonschema:"explicit Gram project slug selected by the user"`
	ProviderKey     string                     `json:"provider_key" jsonschema:"server-issued catalogue source identity returned by search_mcp_catalog"`
	CatalogRef      string                     `json:"catalog_ref" jsonschema:"exact catalogue reference returned by search_mcp_catalog"`
	NonSecretConfig CatalogConfigurationValues `json:"non_secret_config,omitempty" jsonschema:"only declared non-secret configuration values keyed by inspect_mcp_candidate configuration field key; never include API keys, tokens, passwords, OAuth codes, client secrets, or secret headers"`
}

type RegisterOnboardingMCPToolOutput struct {
	ProjectSlug         string                      `json:"project_slug"`
	RegistrationID      string                      `json:"registration_id"`
	NextAction          string                      `json:"next_action"`
	Message             string                      `json:"message"`
	DashboardSetupURL   string                      `json:"dashboard_setup_url,omitempty"`
	SecretFieldsPending []CatalogConfigurationField `json:"secret_fields_pending,omitempty"`
}

type onboardingLifecycleErrorResult struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type GetOnboardingMCPStatusToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit Gram project slug selected by the user"`
	Force       bool   `json:"force,omitempty" jsonschema:"force one authenticated readiness recheck after secure dashboard setup or provider authorization; limited to three probes per minute for this registration"`
}
type GetOnboardingMCPStatusToolOutput struct {
	ProjectSlug    string `json:"project_slug"`
	RegistrationID string `json:"registration_id,omitempty"`
	Registered     bool   `json:"registered"`
	Readiness      string `json:"readiness"`
	Freshness      string `json:"freshness"`
	EvidenceCode   string `json:"evidence_code,omitempty"`
	NextAction     string `json:"next_action"`
	Message        string `json:"message"`
}
type AttachOnboardingMCPIdentityProviderToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit Gram project slug selected by the user"`
	Confirmed   bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms that Gram may attach the reviewed MCP's discovered remote identity provider; never include credentials, client secrets, OAuth codes, or tokens"`
}
type AttachOnboardingMCPIdentityProviderToolOutput struct {
	ProjectSlug      string `json:"project_slug"`
	RegistrationID   string `json:"registration_id"`
	Attached         bool   `json:"attached"`
	ProviderURL      string `json:"provider_url,omitempty"`
	NextAction       string `json:"next_action"`
	Message          string `json:"message"`
	AuthorizationURL string `json:"authorization_url,omitempty"`
}
type DistributeOnboardingMCPToolInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"explicit Gram project slug selected by the user"`
}
type DistributeOnboardingMCPToolOutput struct {
	ProjectSlug      string `json:"project_slug"`
	Attached         bool   `json:"attached"`
	PublicationState string `json:"publication_state"`
	Message          string `json:"message"`
}

func registerOnboardingLifecycleTools(reg *Registrar, onboarding *OnboardingService, registrations *RegistrationService, distributions *DistributionService) {
	addTool(reg, &mcp.Tool{Name: "register_platform_mcp_for_project", Title: "Register Catalogue MCP for Project", Description: "Register a reviewed MCP Catalogue server for an explicit project. Use the exact candidate returned by search_mcp_catalog and inspect_mcp_candidate. Supply declared non-secret values only; required secrets stay in secure dashboard setup. Registration is private and does not distribute or publish the MCP."}, ToolMeta{
		// External-only: onboarding milestones are connection-scoped, which a
		// connection-less surface cannot satisfy.
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input RegisterOnboardingMCPToolInput) (*mcp.CallToolResult, RegisterOnboardingMCPToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, RegisterOnboardingMCPToolOutput{}, err
		}
		if input.ProjectSlug == "" || input.ProviderKey == "" || input.CatalogRef == "" {
			return nil, RegisterOnboardingMCPToolOutput{}, ErrOnboardingInvalid
		}
		projection, err := onboarding.Start(ctx, principal.OrganizationID, principal.UserID)
		if err != nil {
			return nil, RegisterOnboardingMCPToolOutput{}, err
		}
		if projection.Workflow == nil {
			return nil, RegisterOnboardingMCPToolOutput{}, ErrUnavailable
		}
		result, err := registrations.RegisterCatalogMCP(ctx, principal, RegisterCatalogMCPInput{ProjectSlug: input.ProjectSlug, ProviderKey: input.ProviderKey, CatalogRef: input.CatalogRef, NonSecretConfig: input.NonSecretConfig, IdempotencyKey: onboardingRegistrationIdempotencyKey(projection.Workflow.ID, input.ProjectSlug, input.ProviderKey, input.CatalogRef)})
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, RegisterOnboardingMCPToolOutput{}, nil
			}
			return nil, RegisterOnboardingMCPToolOutput{}, err
		}
		registrationID, err := uuid.Parse(result.Registration)
		if err != nil {
			return nil, RegisterOnboardingMCPToolOutput{}, ErrUnavailable
		}
		if _, err := onboarding.BindRegistrationForPrincipal(ctx, principal, result.Project.ID, registrationID); err != nil {
			return nil, RegisterOnboardingMCPToolOutput{}, err
		}
		if err := onboarding.RecordRegistrationSucceeded(ctx, principal, result.Project.ID, registrationID); err != nil {
			return nil, RegisterOnboardingMCPToolOutput{}, err
		}
		nextAction, message := "continue_dashboard_setup", "The reviewed MCP Catalogue server is registered privately for this project. Complete its dashboard setup, then return to check readiness and add it to the existing Default plugin."
		if len(result.SecretFieldsPending) > 0 {
			nextAction = "secure_dashboard_setup_required"
			message = "The reviewed MCP Catalogue server is registered privately. The user must enter the required secret fields in secure dashboard setup before the agent can continue."
		}
		dashboardSetupURL := ""
		if isBrowserCatalogProviderKey(result.ProviderKey) {
			dashboardSetupURL, err = registrations.DashboardSetupURL(ctx, principal, IssueSetupHandoffInput{ProjectSlug: result.Project.Slug, RegistrationID: result.Registration, ProviderKey: result.ProviderKey, CatalogRef: result.CatalogRef})
			if err != nil {
				return nil, RegisterOnboardingMCPToolOutput{}, err
			}
		}
		return nil, RegisterOnboardingMCPToolOutput{ProjectSlug: input.ProjectSlug, RegistrationID: registrationID.String(), NextAction: nextAction, Message: message, DashboardSetupURL: dashboardSetupURL, SecretFieldsPending: append([]CatalogConfigurationField(nil), result.SecretFieldsPending...)}, nil
	})

	addTool(reg, &mcp.Tool{Name: "get_platform_mcp_onboarding_status", Title: "Get Platform MCP Onboarding Status", Description: "Check the workflow-bound MCP Catalogue server setup status for an explicit project. This returns bounded readiness facts, the registration ID, and an actionable next step. When there is no readiness evidence yet, it performs one rate-limited authenticated probe. Set force after the user completes dashboard setup or provider authorization to recheck fresh readiness."}, ToolMeta{
		// External-only: onboarding milestones are connection-scoped, which a
		// connection-less surface cannot satisfy.
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetOnboardingMCPStatusToolInput) (*mcp.CallToolResult, GetOnboardingMCPStatusToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GetOnboardingMCPStatusToolOutput{}, err
		}
		projection, err := onboarding.Get(ctx, principal.OrganizationID, principal.UserID)
		if err != nil {
			return nil, GetOnboardingMCPStatusToolOutput{}, err
		}
		if projection.Workflow == nil || projection.SelectedProject == nil || projection.SelectedProject.Slug != input.ProjectSlug || projection.Workflow.SelectedRegistrationID == uuid.Nil {
			project, err := registrations.store.ResolveProject(ctx, principal.OrganizationID, input.ProjectSlug)
			if err != nil {
				return nil, GetOnboardingMCPStatusToolOutput{}, err
			}
			eligible, err := registrations.store.EligibleCatalogRegistrationTarget(ctx, principal.OrganizationID, project)
			if err != nil {
				return nil, GetOnboardingMCPStatusToolOutput{}, err
			}
			if !eligible {
				content, marshalErr := json.Marshal(onboardingLifecycleErrorResult{Code: "ineligible_project", Message: "This project already has an active legacy toolset-backed MCP, so it cannot use Platform MCP catalogue registration."})
				if marshalErr != nil {
					return nil, GetOnboardingMCPStatusToolOutput{}, marshalErr
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, GetOnboardingMCPStatusToolOutput{}, nil
			}
			return nil, GetOnboardingMCPStatusToolOutput{ProjectSlug: input.ProjectSlug, Readiness: "unknown", Freshness: "unavailable", NextAction: "register_platform_mcp_for_project", Message: "No MCP Catalogue server is registered for this project in the active onboarding workflow."}, nil
		}
		if registrations.readiness == nil {
			return nil, GetOnboardingMCPStatusToolOutput{}, ErrUnavailable
		}
		registrationID := projection.Workflow.SelectedRegistrationID.String()
		_, readiness, found, err := registrations.readiness.CurrentReadiness(ctx, principal, input.ProjectSlug, registrationID)
		if err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, GetOnboardingMCPStatusToolOutput{}, nil
			}
			return nil, GetOnboardingMCPStatusToolOutput{}, err
		}
		if !found || input.Force {
			_, readiness, found, err = registrations.readiness.GetReadiness(ctx, principal, input.ProjectSlug, registrationID, !found || input.Force)
			if err != nil {
				if budgetResult, ok := operationBudgetToolResult(err); ok {
					return budgetResult, GetOnboardingMCPStatusToolOutput{}, nil
				}
				return nil, GetOnboardingMCPStatusToolOutput{}, err
			}
		}
		normalized := normalizedReadiness(readiness, found)
		if normalized.State == ReadinessReady && readinessFreshness(readiness, found) == "fresh" {
			if err := onboarding.RecordReadinessVerified(ctx, principal, projection.SelectedProject.ID, projection.Workflow.SelectedRegistrationID); err != nil {
				return nil, GetOnboardingMCPStatusToolOutput{}, err
			}
		}
		nextAction, message := onboardingReadinessNextAction(normalized, found)
		return nil, GetOnboardingMCPStatusToolOutput{ProjectSlug: input.ProjectSlug, RegistrationID: registrationID, Registered: true, Readiness: string(normalized.State), Freshness: readinessFreshness(readiness, found), EvidenceCode: normalized.EvidenceCode, NextAction: nextAction, Message: message}, nil
	})

	addTool(reg, &mcp.Tool{Name: "attach_platform_mcp_identity_provider", Title: "Attach Platform MCP Identity Provider", Description: "Attach the workflow-bound MCP's one discovered remote identity provider for an explicit project. Ask for explicit user confirmation before calling this tool. It derives provider metadata and dynamic client registration from the persisted reviewed MCP source. Non-secret provider URLs may be returned, but it never accepts or returns credentials, OAuth codes, tokens, client secrets, passwords, or API keys. After success, immediately present authorization_url as a clickable link and tell the user to open it and use Connect or Authorize; do not ask whether they completed an unspecified action or refer to a link above."}, ToolMeta{
		// External-only: provider setup is connection-scoped, which a
		// connection-less surface cannot satisfy.
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input AttachOnboardingMCPIdentityProviderToolInput) (*mcp.CallToolResult, AttachOnboardingMCPIdentityProviderToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, AttachOnboardingMCPIdentityProviderToolOutput{}, err
		}
		if input.ProjectSlug == "" {
			return nil, AttachOnboardingMCPIdentityProviderToolOutput{}, ErrOnboardingInvalid
		}
		if !input.Confirmed {
			content, marshalErr := json.Marshal(onboardingLifecycleErrorResult{Code: "confirmation_required", Message: "Ask the user to explicitly confirm that Gram may attach the reviewed MCP's discovered remote identity provider, then call this tool again with confirmed: true."})
			if marshalErr != nil {
				return nil, AttachOnboardingMCPIdentityProviderToolOutput{}, marshalErr
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, AttachOnboardingMCPIdentityProviderToolOutput{}, nil
		}
		projection, err := onboarding.Get(ctx, principal.OrganizationID, principal.UserID)
		if err != nil {
			return nil, AttachOnboardingMCPIdentityProviderToolOutput{}, err
		}
		if projection.Workflow == nil || projection.SelectedProject == nil || projection.SelectedProject.Slug != input.ProjectSlug || projection.Workflow.SelectedRegistrationID == uuid.Nil {
			return nil, AttachOnboardingMCPIdentityProviderToolOutput{}, ErrOnboardingInvalid
		}
		registrationID := projection.Workflow.SelectedRegistrationID.String()
		attachment, err := registrations.AttachDefaultIdentityProvider(ctx, principal, input.ProjectSlug, registrationID)
		if err != nil {
			if result, ok := onboardingIdentityProviderAttachmentError(err); ok {
				return result, AttachOnboardingMCPIdentityProviderToolOutput{}, nil
			}
			return onboardingIdentityProviderAttachmentUnavailableResult()
		}
		authorizationURL, err := registrations.DashboardAuthorizationURL(ctx, principal, input.ProjectSlug, registrationID)
		if err != nil {
			return onboardingIdentityProviderAttachmentAuthorizationUnavailableResult()
		}
		return nil, onboardingIdentityProviderAttachmentOutput(input.ProjectSlug, registrationID, attachment, authorizationURL), nil
	})

	addTool(reg, &mcp.Tool{Name: "add_platform_mcp_to_default_plugin", Title: "Add Platform MCP to Default Plugin", Description: "Add the workflow-bound, ready MCP Catalogue server to the explicit project's existing Default plugin. The project must already be selected, registered, and freshly ready through the dashboard setup flow. This never creates a plugin."}, ToolMeta{
		// External-only: distribution rows require a connection, which a
		// connection-less surface cannot satisfy.
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input DistributeOnboardingMCPToolInput) (*mcp.CallToolResult, DistributeOnboardingMCPToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, DistributeOnboardingMCPToolOutput{}, err
		}
		if input.ProjectSlug == "" {
			return nil, DistributeOnboardingMCPToolOutput{}, ErrDistributionInvalid
		}
		distribution, err := distributions.DistributeForOnboarding(ctx, principal, input.ProjectSlug)
		if err != nil {
			if result, ok := onboardingLifecycleToolError(err); ok {
				return result, DistributeOnboardingMCPToolOutput{}, nil
			}
			return nil, DistributeOnboardingMCPToolOutput{}, err
		}
		if distribution.AttachmentLive {
			projection, err := onboarding.Get(ctx, principal.OrganizationID, principal.UserID)
			if err != nil {
				return nil, DistributeOnboardingMCPToolOutput{}, err
			}
			if projection.Workflow == nil || projection.SelectedProject == nil || projection.SelectedProject.Slug != input.ProjectSlug || projection.Workflow.SelectedRegistrationID == uuid.Nil {
				return nil, DistributeOnboardingMCPToolOutput{}, ErrUnavailable
			}
			if err := onboarding.RecordDistributionSucceeded(ctx, principal, projection.SelectedProject.ID, projection.Workflow.SelectedRegistrationID); err != nil {
				return nil, DistributeOnboardingMCPToolOutput{}, err
			}
		}
		return nil, DistributeOnboardingMCPToolOutput{ProjectSlug: input.ProjectSlug, Attached: distribution.AttachmentLive, PublicationState: distribution.PublicationState, Message: "The MCP Catalogue server is in the project's existing Default plugin. Install or refresh that project's package, then call one selected-project MCP tool to verify it works."}, nil
	})
}

func onboardingIdentityProviderAttachmentOutput(projectSlug, registrationID string, attachment CatalogIdentityProviderAttachmentResult, authorizationURL string) AttachOnboardingMCPIdentityProviderToolOutput {
	return AttachOnboardingMCPIdentityProviderToolOutput{ProjectSlug: projectSlug, RegistrationID: registrationID, Attached: attachment.Attached, ProviderURL: attachment.ProviderURL, NextAction: "open_authorization_url", Message: "The discovered remote identity provider is attached. Open this Inspect authorization link now: " + authorizationURL + ". Use Connect or Authorize there, then force a fresh onboarding status check.", AuthorizationURL: authorizationURL}
}
func onboardingReadinessNextAction(readiness Readiness, found bool) (string, string) {
	if !found {
		return "retry_readiness", "No readiness evidence is available yet. Retry the authenticated readiness check."
	}
	switch readiness.EvidenceCode {
	case "upstream_identity_provider_not_configured":
		return "attach_platform_mcp_identity_provider", "Ask the user to confirm that Gram may attach the reviewed MCP's discovered remote identity provider, then call attach_platform_mcp_identity_provider. Provider URLs are safe to show; never ask for credentials, OAuth codes, or tokens in chat."
	case "multiple_upstream_identity_providers":
		return "select_upstream_identity_provider", "Open the secure dashboard setup and select exactly one upstream identity provider for this MCP, then recheck readiness."
	case "upstream_authorization_required":
		return "open_authorization_url", "The identity provider is attached. Call attach_platform_mcp_identity_provider again to retrieve the server-issued Inspect authorization URL, then present that exact clickable URL to the user. They must use Connect or Authorize there before readiness is rechecked with force. Do not paste OAuth codes or tokens in chat."
	}
	switch readiness.State {
	case ReadinessReady:
		return "add_platform_mcp_to_default_plugin", "The MCP is freshly ready. Add it to the project's existing Default plugin."
	case ReadinessNeedsConfiguration:
		return "continue_secure_setup", "Open the secure dashboard setup and complete the required non-chat configuration, then recheck readiness."
	case ReadinessNeedsProviderSetup, ReadinessNeedsGramAuthorization, ReadinessAuthFailed, ReadinessUnauthorized:
		return "continue_secure_setup", "Open the secure dashboard setup and complete the required provider authentication, then recheck readiness."
	case ReadinessUnreachable, ReadinessUnsupported, ReadinessDegraded:
		return "retry_readiness", "The authenticated readiness check is not currently successful. Retry the supported readiness check after the delay."
	case ReadinessGuideUnavailable:
		return "contact_support", "The reviewed setup guidance is unavailable. Ask an administrator to restore it."
	default:
		return "retry_readiness", "Retry the authenticated readiness check."
	}
}
func onboardingIdentityProviderAttachmentUnavailableResult() (*mcp.CallToolResult, AttachOnboardingMCPIdentityProviderToolOutput, error) {
	content, err := json.Marshal(onboardingLifecycleErrorResult{Code: unavailableCode, Message: "Automatic identity-provider attachment is temporarily unavailable. No provider change was confirmed. Retry later or use the server-issued Authentication settings page."})
	if err != nil {
		return nil, AttachOnboardingMCPIdentityProviderToolOutput{}, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, AttachOnboardingMCPIdentityProviderToolOutput{}, nil
}
func onboardingIdentityProviderAttachmentAuthorizationUnavailableResult() (*mcp.CallToolResult, AttachOnboardingMCPIdentityProviderToolOutput, error) {
	content, err := json.Marshal(onboardingLifecycleErrorResult{Code: "identity_provider_attached_authorization_url_unavailable", Message: "The discovered remote identity provider was attached, but Gram could not create the server-issued Inspect authorization link. Open the registered MCP's Inspect page and use Connect or Authorize, then force a fresh onboarding status check."})
	if err != nil {
		return nil, AttachOnboardingMCPIdentityProviderToolOutput{}, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, AttachOnboardingMCPIdentityProviderToolOutput{}, nil
}

func onboardingIdentityProviderAttachmentError(err error) (*mcp.CallToolResult, bool) {
	var result onboardingLifecycleErrorResult
	switch {
	case errors.Is(err, ErrIdentityProviderAttachmentUnsupported):
		result = onboardingLifecycleErrorResult{Code: "automatic_identity_provider_attachment_unsupported", Message: "This reviewed MCP does not advertise exactly one identity provider with supported OAuth metadata and dynamic client registration. Automatic attachment was not performed. Explain this limitation to the user and ask how they want to proceed."}
	case errors.Is(err, ErrIdentityProviderAttachmentConflict):
		result = onboardingLifecycleErrorResult{Code: "identity_provider_attachment_conflict", Message: "This MCP already has a different or ambiguous remote identity-provider configuration. Automatic attachment was not performed. Ask the user how they want to proceed."}
	case errors.Is(err, ErrIdentityProviderAttachmentUnavailable), errors.Is(err, ErrRegistrationUnavailable), errors.Is(err, ErrOperationRateLimited), errors.Is(err, ErrOperationBudgetUnavailable):
		return nil, false
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
func onboardingLifecycleToolError(err error) (*mcp.CallToolResult, bool) {
	var result onboardingLifecycleErrorResult
	switch {
	case errors.Is(err, ErrDistributionNotReady):
		result = onboardingLifecycleErrorResult{Code: "not_ready", Message: "Complete secure setup and recheck fresh readiness in the Gram dashboard before adding this MCP."}
	case errors.Is(err, ErrDistributionDefaultAbsent):
		result = onboardingLifecycleErrorResult{Code: "default_plugin_missing", Message: "This project does not have an existing Default plugin, so Platform MCP cannot add the MCP."}
	case errors.Is(err, ErrDistributionTargetUnavailable):
		result = onboardingLifecycleErrorResult{Code: "distribution_target_unavailable", Message: "The selected project or its Platform MCP setup is no longer available. Check onboarding status and choose the supported next action."}
	case errors.Is(err, ErrDistributionConflict):
		result = onboardingLifecycleErrorResult{Code: "conflict", Message: "The project distribution changed. Check onboarding status and retry the supported next action."}
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
