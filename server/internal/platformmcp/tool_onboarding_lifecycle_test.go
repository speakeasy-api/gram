package platformmcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestOnboardingReadinessNextActionExplainsSecureSetupRequirements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		readiness  Readiness
		found      bool
		nextAction string
		message    string
	}{
		{
			name:       "missing identity provider",
			readiness:  Readiness{State: ReadinessNeedsConfiguration, EvidenceCode: "upstream_identity_provider_not_configured"},
			found:      true,
			nextAction: "attach_platform_mcp_identity_provider",
			message:    "Ask the user to confirm",
		},
		{
			name:       "provider authorization required",
			readiness:  Readiness{State: ReadinessNeedsGramAuthorization, EvidenceCode: "upstream_authorization_required"},
			found:      true,
			nextAction: "open_authorization_url",
			message:    "Inspect authorization URL",
		},
		{
			name:       "ready for distribution",
			readiness:  Readiness{State: ReadinessReady},
			found:      true,
			nextAction: "add_platform_mcp_to_default_plugin",
			message:    "freshly ready",
		},
		{
			name:       "no evidence",
			readiness:  Readiness{},
			found:      false,
			nextAction: "retry_readiness",
			message:    "No readiness evidence",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			nextAction, message := onboardingReadinessNextAction(test.readiness, test.found)

			require.Equal(t, test.nextAction, nextAction)
			require.Contains(t, message, test.message)
		})
	}
}

func TestOnboardingIdentityProviderAttachmentOutputPresentsExactAuthorizationLink(t *testing.T) {
	t.Parallel()

	output := onboardingIdentityProviderAttachmentOutput(
		"project",
		"registration",
		CatalogIdentityProviderAttachmentResult{Attached: true, ProviderURL: "https://provider.example"},
		"https://dashboard.example/organization/projects/project/mcp/x/server/inspect",
	)

	require.Equal(t, "open_authorization_url", output.NextAction)
	require.Equal(t, "https://dashboard.example/organization/projects/project/mcp/x/server/inspect", output.AuthorizationURL)
	require.Contains(t, output.Message, "Open this Inspect authorization link now: "+output.AuthorizationURL)
	require.Contains(t, output.Message, "Connect or Authorize")
	require.NotContains(t, output.Message, "above")
}

func TestOnboardingIdentityProviderAttachmentReturnsOnlyBoundedResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		code    string
		message string
	}{
		{
			name:    "unsupported provider contract",
			err:     ErrIdentityProviderAttachmentUnsupported,
			code:    "automatic_identity_provider_attachment_unsupported",
			message: "This reviewed MCP does not advertise exactly one identity provider with supported OAuth metadata and dynamic client registration. Automatic attachment was not performed. Explain this limitation to the user and ask how they want to proceed.",
		},
		{
			name:    "existing attachment conflict",
			err:     ErrIdentityProviderAttachmentConflict,
			code:    "identity_provider_attachment_conflict",
			message: "This MCP already has a different or ambiguous remote identity-provider configuration. Automatic attachment was not performed. Ask the user how they want to proceed.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, ok := onboardingIdentityProviderAttachmentError(test.err)

			require.True(t, ok)
			require.True(t, result.IsError)
			require.Len(t, result.Content, 1)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			require.JSONEq(t, `{"code":"`+test.code+`","message":"`+test.message+`"}`, text.Text)
		})
	}

	result, ok := onboardingIdentityProviderAttachmentError(ErrIdentityProviderAttachmentUnavailable)
	require.False(t, ok)
	require.Nil(t, result)
}

func TestOnboardingStatusOutputHasExplicitNoRegistrationValues(t *testing.T) {
	t.Parallel()

	output := GetOnboardingMCPStatusToolOutput{
		ProjectSlug: "project",
		Readiness:   "unknown",
		Freshness:   "unavailable",
		NextAction:  "register_platform_mcp_for_project",
	}

	require.Equal(t, "unknown", output.Readiness)
	require.Equal(t, "unavailable", output.Freshness)
	require.Empty(t, output.RegistrationID)
}
