//nolint:exhaustruct,wrapcheck // MCP SDK manifests use optional zero values and preserve bounded typed errors.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AttachPlatformMCPIdentityProviderToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit project slug that owns the reviewed MCP registration"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform MCP registration ID returned by register_catalog_mcp or register_remote_mcp"`
	Confirmed      bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms that this MCP server may be connected to the sign-in provider it advertises; never include credentials, client secrets, OAuth codes, or tokens"`
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
		Title:       "Connect an MCP Server's OAuth Provider",
		Description: "Connect one MCP server to the OAuth provider it advertises. This is not switched on for your organization yet.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("identity_provider_attachment"))
}

func registerIdentityProviderTool(reg *Registrar, registrations *RegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:        "attach_platform_mcp_identity_provider",
		Title:       "Connect an MCP Server's OAuth Provider",
		Description: "Connect one MCP server to the OAuth provider it advertises, so people can sign in to it. Ask for explicit user confirmation before calling this tool. It works out the provider's metadata and performs dynamic client registration from the stored MCP source. Constraints: non-secret provider URLs may be returned, but it never accepts or returns credentials, OAuth codes, tokens, client secrets, passwords, or API keys. After success, immediately present authorization_url as a clickable link and tell the user to open it and use Connect or Authorize.",
	}, ToolMeta{
		// Connection-less: the registration is resolved by user and project,
		// the operation is serialised per user, and the authorization step the
		// result hands back is a dashboard URL the user opens under their own
		// session. Nothing here needs the caller's OAuth connection.
		Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AttachPlatformMCPIdentityProviderToolInput) (*mcp.CallToolResult, AttachPlatformMCPIdentityProviderToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, AttachPlatformMCPIdentityProviderToolOutput{}, err
		}
		if input.ProjectSlug == "" || input.RegistrationID == "" {
			return nil, AttachPlatformMCPIdentityProviderToolOutput{}, ErrOnboardingInvalid
		}
		if !input.Confirmed {
			content, marshalErr := json.Marshal(identityProviderAttachmentErrorResult{Code: "confirmation_required", Message: "Ask the user to confirm out loud that this MCP server may be connected to the sign-in provider it advertises, then call this tool again with confirmed: true."})
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
	return AttachPlatformMCPIdentityProviderToolOutput{ProjectSlug: projectSlug, RegistrationID: registrationID, Attached: attachment.Attached, ProviderURL: attachment.ProviderURL, NextAction: "open_authorization_url", Message: "This MCP server is now connected to its sign-in provider. Open this link to authorize it: " + authorizationURL + ". Use Connect or Authorize there, then check the MCP server again.", AuthorizationURL: authorizationURL}
}

func identityProviderAttachmentUnavailableResult() (*mcp.CallToolResult, AttachPlatformMCPIdentityProviderToolOutput, error) {
	content, err := json.Marshal(identityProviderAttachmentErrorResult{Code: unavailableCode, Message: "Connecting the sign-in provider automatically is temporarily unavailable, and nothing was changed. Try again later, or set it up on the Authentication settings page."})
	if err != nil {
		return nil, AttachPlatformMCPIdentityProviderToolOutput{}, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, AttachPlatformMCPIdentityProviderToolOutput{}, nil
}

func identityProviderAttachmentAuthorizationUnavailableResult() (*mcp.CallToolResult, AttachPlatformMCPIdentityProviderToolOutput, error) {
	content, err := json.Marshal(identityProviderAttachmentErrorResult{Code: "identity_provider_attached_authorization_url_unavailable", Message: "This MCP server is connected to its sign-in provider, but the authorization link could not be created. Open the MCP server's page in the dashboard, use Connect or Authorize, then check it again."})
	if err != nil {
		return nil, AttachPlatformMCPIdentityProviderToolOutput{}, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, AttachPlatformMCPIdentityProviderToolOutput{}, nil
}

func identityProviderAttachmentError(err error) (*mcp.CallToolResult, bool) {
	var result identityProviderAttachmentErrorResult
	switch {
	case errors.Is(err, ErrIdentityProviderAttachmentUnsupported):
		result = identityProviderAttachmentErrorResult{Code: "automatic_identity_provider_attachment_unsupported", Message: "This MCP server does not advertise exactly one sign-in provider with the OAuth metadata and dynamic client registration needed to set it up automatically, so nothing was changed. Explain that to the user and ask how they want to proceed."}
	case errors.Is(err, ErrIdentityProviderAttachmentConflict):
		result = identityProviderAttachmentErrorResult{Code: "identity_provider_attachment_conflict", Message: "This MCP server already has a different or unclear sign-in provider set up, so nothing was changed. Ask the user how they want to proceed."}
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
