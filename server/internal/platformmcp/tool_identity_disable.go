//nolint:exhaustruct,wrapcheck // MCP SDK manifests use optional zero values and preserve bounded typed errors.
package platformmcp

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type TurnOffMCPSignInToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit project slug that owns the MCP server"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform registration ID returned by find_mcp or get_mcp"`
	Confirmed      bool   `json:"confirmed" jsonschema:"false returns what the change would do and makes none; true performs it after the user has agreed to that result out loud"`
}

type TurnOffMCPSignInToolOutput struct {
	ProjectSlug    string `json:"project_slug"`
	RegistrationID string `json:"registration_id"`
	// SignInConfigured reports whether the server signs in to a provider at all.
	SignInConfigured bool `json:"sign_in_configured"`
	// ProviderURL is the non-secret issuer URL of that provider.
	ProviderURL string `json:"provider_url,omitempty"`
	// Applied is false for a preview and true once the change is made.
	Applied bool `json:"applied"`
	// SignsInToNothing reports the state the server is left in.
	SignsInToNothing bool `json:"signs_in_to_nothing"`
	// OtherServersKeepTheClient reports that another MCP server still signs in
	// through this provider's OAuth client, so it stays available to them.
	OtherServersKeepTheClient bool `json:"other_servers_keep_the_client"`
	// PeopleMustSignInAgain reports that the OAuth client was unbound, which
	// ends every session held through it.
	PeopleMustSignInAgain bool   `json:"people_must_sign_in_again"`
	NextAction            string `json:"next_action"`
	Message               string `json:"message"`
}

func registerUnavailableTurnOffMCPSignInTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "turn_off_mcp_sign_in",
		Title:       "Turn Off an MCP Server's Sign-In",
		Description: "Stop one MCP server asking people to sign in to its service. This is not switched on for your organization yet.",
	}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("identity_provider_attachment"))
}

func registerTurnOffMCPSignInTool(reg *Registrar, registrations *RegistrationService) {
	addTool(reg, &mcp.Tool{
		Name:  "turn_off_mcp_sign_in",
		Title: "Turn Off an MCP Server's Sign-In",
		Description: "Stop one MCP server asking people to sign in to the service behind it, so it calls that service without anyone's account. " +
			"Call it first with confirmed: false to get exactly what would happen, show that to the user, and call again with confirmed: true only after they agree out loud. " +
			"It affects the named server only: the provider's OAuth client is kept when another MCP server still signs in through it, and unbound otherwise, which ends every session held through it. " +
			"Each server's sign-in is configured on that server, so turning it off is not undone by anything else and switching it back on is a separate setup. " +
			"Retrying is safe: a server that already signs in to nothing is reported as such and nothing changes. " +
			"Constraints: non-secret provider URLs may be returned, but it never accepts or returns credentials, OAuth codes, tokens, client secrets, passwords, or API keys.",
	}, ToolMeta{
		Authorization: ExternalAuthorizationOrgAdmin,
		// Connection-less, like the attach tool it reverses: the registration
		// is resolved by user and project and the operation runs server-side
		// under the caller's organization admin rights.
		Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input TurnOffMCPSignInToolInput) (*mcp.CallToolResult, TurnOffMCPSignInToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, TurnOffMCPSignInToolOutput{}, err
		}
		if input.ProjectSlug == "" || input.RegistrationID == "" {
			return nil, TurnOffMCPSignInToolOutput{}, ErrOnboardingInvalid
		}

		preview, err := registrations.PreviewIdentityProviderRemoval(ctx, principal, input.ProjectSlug, input.RegistrationID)
		if err != nil {
			return nil, TurnOffMCPSignInToolOutput{}, err
		}
		if !preview.Configured {
			return nil, TurnOffMCPSignInToolOutput{
				ProjectSlug: input.ProjectSlug, RegistrationID: input.RegistrationID,
				SignInConfigured: false, Applied: input.Confirmed, SignsInToNothing: true,
				NextAction: "none",
				Message:    "This MCP server already signs in to nothing. Nothing was changed.",
			}, nil
		}
		if !input.Confirmed {
			return nil, TurnOffMCPSignInToolOutput{
				ProjectSlug: input.ProjectSlug, RegistrationID: input.RegistrationID,
				SignInConfigured: true, ProviderURL: preview.ProviderURL,
				Applied: false, SignsInToNothing: false,
				OtherServersKeepTheClient: preview.ClientStaysBound,
				PeopleMustSignInAgain:     !preview.ClientStaysBound,
				NextAction:                "confirm",
				Message:                   signInRemovalPreviewMessage(preview),
			}, nil
		}

		result, err := registrations.RemoveIdentityProvider(ctx, principal, input.ProjectSlug, input.RegistrationID)
		if err != nil {
			content, marshalErr := json.Marshal(identityProviderAttachmentErrorResult{Code: "sign_in_removal_refused", Message: err.Error()})
			if marshalErr != nil {
				return nil, TurnOffMCPSignInToolOutput{}, marshalErr
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, TurnOffMCPSignInToolOutput{}, nil
		}
		// Read back what landed rather than reporting the plan: the removal is
		// only done when the server signs in to nothing.
		after, err := registrations.PreviewIdentityProviderRemoval(ctx, principal, input.ProjectSlug, input.RegistrationID)
		if err != nil {
			return nil, TurnOffMCPSignInToolOutput{}, err
		}
		return nil, TurnOffMCPSignInToolOutput{
			ProjectSlug: input.ProjectSlug, RegistrationID: input.RegistrationID,
			SignInConfigured: true, ProviderURL: result.ProviderURL,
			Applied: true, SignsInToNothing: !after.Configured,
			OtherServersKeepTheClient: !result.ClientUnbound,
			PeopleMustSignInAgain:     result.ClientUnbound,
			NextAction:                "none",
			Message:                   "This MCP server now signs in to nothing and calls its service without anyone's account.",
		}, nil
	})
}

func signInRemovalPreviewMessage(preview SignInRemovalPreview) string {
	if preview.ClientStaysBound {
		return "Turning this off stops the server asking people to sign in. Another MCP server still signs in through the same OAuth client, so that client is kept and those people stay signed in to it."
	}
	return "Turning this off stops the server asking people to sign in, and unbinds its OAuth client, so everyone signed in through it has to sign in again if it is ever set up afresh."
}
