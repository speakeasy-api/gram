//nolint:exhaustruct,wrapcheck // MCP SDK manifests use optional zero values and preserve bounded typed errors.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AttachPlatformMCPIdentityProviderToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit AICP project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_catalog_mcp or register_remote_mcp"`
	Confirmed      bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms that the AI Control Plane may attach the reviewed MCP's discovered remote identity provider; never include credentials, client secrets, OAuth codes, or tokens"`
}

type AttachPlatformMCPIdentityProviderToolOutput struct {
	ProjectSlug      string `json:"project_slug"`
	RegistrationID   string `json:"registration_id"`
	Attached         bool   `json:"attached"`
	ProviderURL      string `json:"provider_url,omitempty"`
	NextAction       string `json:"next_action"`
	Message          string `json:"message"`
	AuthorizationURL string `json:"authorization_url,omitempty"`
}

type identityProviderAttachmentErrorResult struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func registerUnavailableIdentityProviderTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "attach_platform_mcp_identity_provider",
		Title:       "Attach Platform MCP Identity Provider",
		Description: "Attach a reviewed MCP's discovered remote identity provider. Provider attachment is not available in the current preview.",
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, unavailableTool("identity_provider_attachment"))
}

func registerIdentityProviderTool(reg *Registrar, registrations *RegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "attach_platform_mcp_identity_provider",
		Title:       "Attach Platform MCP Identity Provider",
		Description: "Attach one reviewed MCP registration's discovered remote identity provider. Ask for explicit user confirmation before calling this tool. It derives provider metadata and dynamic client registration from the persisted MCP source. Non-secret provider URLs may be returned, but it never accepts or returns credentials, OAuth codes, tokens, client secrets, passwords, or API keys. After success, immediately present authorization_url as a clickable link and tell the user to open it and use Connect or Authorize.",
	}, ToolMeta{
		// Provider setup is connection-scoped, which a connection-less surface
		// cannot satisfy. The assistant returns the normal dashboard handoff.
		Audiences: externalOnly, ProjectScope: ProjectScopeExplicit,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AttachPlatformMCPIdentityProviderToolInput) (*mcp.CallToolResult, AttachPlatformMCPIdentityProviderToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, AttachPlatformMCPIdentityProviderToolOutput{}, err
		}
		if input.ProjectSlug == "" || input.RegistrationID == "" {
			return nil, AttachPlatformMCPIdentityProviderToolOutput{}, ErrOnboardingInvalid
		}
		if !input.Confirmed {
			content, marshalErr := json.Marshal(identityProviderAttachmentErrorResult{Code: "confirmation_required", Message: "Ask the user to explicitly confirm that the AI Control Plane may attach the reviewed MCP's discovered remote identity provider, then call this tool again with confirmed: true."})
			if marshalErr != nil {
				return nil, AttachPlatformMCPIdentityProviderToolOutput{}, marshalErr
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, AttachPlatformMCPIdentityProviderToolOutput{}, nil
		}

		attachment, err := registrations.AttachDefaultIdentityProvider(ctx, principal, input.ProjectSlug, input.RegistrationID)
		if err != nil {
			if result, ok := identityProviderAttachmentError(err); ok {
				return result, AttachPlatformMCPIdentityProviderToolOutput{}, nil
			}
			return identityProviderAttachmentUnavailableResult()
		}
		authorizationURL, err := registrations.DashboardAuthorizationURL(ctx, principal, input.ProjectSlug, input.RegistrationID)
		if err != nil {
			return identityProviderAttachmentAuthorizationUnavailableResult()
		}
		return nil, identityProviderAttachmentOutput(input.ProjectSlug, input.RegistrationID, attachment, authorizationURL), nil
	})
}

func identityProviderAttachmentOutput(projectSlug, registrationID string, attachment CatalogIdentityProviderAttachmentResult, authorizationURL string) AttachPlatformMCPIdentityProviderToolOutput {
	return AttachPlatformMCPIdentityProviderToolOutput{ProjectSlug: projectSlug, RegistrationID: registrationID, Attached: attachment.Attached, ProviderURL: attachment.ProviderURL, NextAction: "open_authorization_url", Message: "The discovered remote identity provider is attached. Open this Inspect authorization link now: " + authorizationURL + ". Use Connect or Authorize there, then force a fresh readiness check.", AuthorizationURL: authorizationURL}
}

func identityProviderAttachmentUnavailableResult() (*mcp.CallToolResult, AttachPlatformMCPIdentityProviderToolOutput, error) {
	content, err := json.Marshal(identityProviderAttachmentErrorResult{Code: unavailableCode, Message: "Automatic identity-provider attachment is temporarily unavailable. No provider change was confirmed. Retry later or use the server-issued Authentication settings page."})
	if err != nil {
		return nil, AttachPlatformMCPIdentityProviderToolOutput{}, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, AttachPlatformMCPIdentityProviderToolOutput{}, nil
}

func identityProviderAttachmentAuthorizationUnavailableResult() (*mcp.CallToolResult, AttachPlatformMCPIdentityProviderToolOutput, error) {
	content, err := json.Marshal(identityProviderAttachmentErrorResult{Code: "identity_provider_attached_authorization_url_unavailable", Message: "The discovered remote identity provider was attached, but the AI Control Plane could not create the server-issued Inspect authorization link. Open the registered MCP's Inspect page and use Connect or Authorize, then force a fresh readiness check."})
	if err != nil {
		return nil, AttachPlatformMCPIdentityProviderToolOutput{}, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, AttachPlatformMCPIdentityProviderToolOutput{}, nil
}

func identityProviderAttachmentError(err error) (*mcp.CallToolResult, bool) {
	var result identityProviderAttachmentErrorResult
	switch {
	case errors.Is(err, ErrIdentityProviderAttachmentUnsupported):
		result = identityProviderAttachmentErrorResult{Code: "automatic_identity_provider_attachment_unsupported", Message: "This reviewed MCP does not advertise exactly one identity provider with supported OAuth metadata and dynamic client registration. Automatic attachment was not performed. Explain this limitation to the user and ask how they want to proceed."}
	case errors.Is(err, ErrIdentityProviderAttachmentConflict):
		result = identityProviderAttachmentErrorResult{Code: "identity_provider_attachment_conflict", Message: "This MCP already has a different or ambiguous remote identity-provider configuration. Automatic attachment was not performed. Ask the user how they want to proceed."}
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
