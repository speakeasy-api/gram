//nolint:exhaustruct,wrapcheck // MCP SDK manifests use optional zero values and preserve bounded typed errors.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

type TurnOffMCPSignInToolInput struct {
	ProjectSlug    string `json:"project_slug" jsonschema:"explicit project slug that owns the MCP server"`
	RegistrationID string `json:"registration_id" jsonschema:"Platform registration ID returned by find_mcp or get_mcp"`
	Confirmed      bool   `json:"confirmed" jsonschema:"false returns what the change would do and makes none; true performs it after the user has agreed to that result out loud"`
	// ExpectedVersion ties a confirmation to the preview it was agreed
	// against, so a sign-in reconfigured in between is never turned off on the
	// strength of an answer about the old one.
	ExpectedVersion string `json:"expected_version,omitempty" jsonschema:"required with confirmed: true; the version returned by the preview call for this same server"`
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
	// Version is the server state this answer describes. Pass it back as
	// expected_version when confirming.
	Version string `json:"version,omitempty"`
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
			"Call it first with confirmed: false to get exactly what would happen, show that to the user, and call again with confirmed: true and expected_version set to the version that preview returned, only after they agree out loud. " +
			"A confirmation is refused when the server's sign-in changed since that preview, or when it has already been carried out: preview again and ask the user about the new answer. " +
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
				SignInConfigured: false, Applied: false, SignsInToNothing: true,
				Version:    preview.Version,
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
				Version:                   preview.Version,
				NextAction:                "confirm",
				Message:                   signInRemovalPreviewMessage(preview),
			}, nil
		}

		result, err := registrations.RemoveIdentityProvider(ctx, principal, input.ProjectSlug, input.RegistrationID, input.ExpectedVersion)
		if errors.Is(err, ErrSignInRemovalStale) {
			content, marshalErr := json.Marshal(identityProviderAttachmentErrorResult{
				Code:    "sign_in_removal_stale",
				Message: "This MCP server's sign-in is not in the state the user agreed to turn off, so nothing was changed. Call this tool again with confirmed: false, show the user what it reports now, and only confirm that.",
			})
			if marshalErr != nil {
				return nil, TurnOffMCPSignInToolOutput{}, marshalErr
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, TurnOffMCPSignInToolOutput{}, nil
		}
		if err != nil {
			if refusal, ok := signInRemovalRefusal(err); ok {
				return refusal, TurnOffMCPSignInToolOutput{}, nil
			}
			// Unclassified: hand it to the SDK rather than describing it, so
			// the project's fail-safe decides what a caller may see.
			return nil, TurnOffMCPSignInToolOutput{}, err
		}
		// Read back what landed rather than reporting the plan: the removal is
		// only done when the server signs in to nothing.
		after, err := registrations.PreviewIdentityProviderRemoval(ctx, principal, input.ProjectSlug, input.RegistrationID)
		if err != nil {
			// The removal already committed and may have signed people out.
			// Reporting a failed call would invite the agent to retry a change
			// that is done, so report what was carried out and say only that
			// reading it back afterwards did not work.
			return nil, TurnOffMCPSignInToolOutput{
				ProjectSlug: input.ProjectSlug, RegistrationID: input.RegistrationID,
				SignInConfigured: false, ProviderURL: result.ProviderURL,
				Applied: true, SignsInToNothing: result.Removed,
				OtherServersKeepTheClient: !result.ClientUnbound,
				PeopleMustSignInAgain:     result.ClientUnbound,
				NextAction:                "none",
				Message:                   "This MCP server's sign-in was turned off. Checking the result afterwards did not work, so tell the user it is done and that its current state could not be read back; do not call this tool again for it.",
			}, nil
		}
		return nil, TurnOffMCPSignInToolOutput{
			ProjectSlug: input.ProjectSlug, RegistrationID: input.RegistrationID,
			SignInConfigured: after.Configured, ProviderURL: result.ProviderURL,
			Applied: true, SignsInToNothing: !after.Configured, Version: after.Version,
			OtherServersKeepTheClient: !result.ClientUnbound,
			PeopleMustSignInAgain:     result.ClientUnbound,
			NextAction:                "none",
			Message:                   "This MCP server now signs in to nothing and calls its service without anyone's account.",
		}, nil
	})
}

// signInRemovalRefusal projects the bounded refusals this operation can answer
// with into messages an agent can act on. Everything else is unclassified and
// must not be described to the caller: the underlying error carries wrapped
// database and driver text.
func signInRemovalRefusal(err error) (*mcp.CallToolResult, bool) {
	var result identityProviderAttachmentErrorResult
	switch {
	case errors.Is(err, remotesessions.ErrIdentityConflict):
		result = identityProviderAttachmentErrorResult{Code: "sign_in_removal_conflict", Message: "This MCP server's sign-in is in use or changed while the request was being made, so nothing was changed. Something may still be bound to its current OAuth client. Ask the user how they want to proceed."}
	case errors.Is(err, remotesessions.ErrIdentityOrgWideBinding):
		result = identityProviderAttachmentErrorResult{Code: "sign_in_removal_organization_wide", Message: "Turning this off would release an OAuth client every project on this organization sign-in shares, so nothing was changed. Tell the user this has to be done from the Remote Identity Provider settings."}
	case errors.Is(err, remotesessions.ErrIdentityForbidden):
		result = identityProviderAttachmentErrorResult{Code: "sign_in_removal_forbidden", Message: "Turning this MCP server's sign-in off is not permitted for this user, so nothing was changed."}
	case errors.Is(err, remotesessions.ErrIdentityNotFound), errors.Is(err, remotesessions.ErrIdentityInvalid), errors.Is(err, ErrRegistrationInvalid):
		result = identityProviderAttachmentErrorResult{Code: "sign_in_removal_invalid", Message: "This MCP server or its sign-in could not be resolved, so nothing was changed. Check the project and registration id with find_mcp or get_mcp."}
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}

func signInRemovalPreviewMessage(preview SignInRemovalPreview) string {
	if preview.ClientStaysBound {
		return "Turning this off stops the server asking people to sign in. Another MCP server still signs in through the same OAuth client, so that client is kept and those people stay signed in to it."
	}
	return "Turning this off stops the server asking people to sign in, and unbinds its OAuth client, so everyone signed in through it has to sign in again if it is ever set up afresh."
}
