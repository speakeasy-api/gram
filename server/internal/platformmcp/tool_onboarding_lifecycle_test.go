package platformmcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestIdentityProviderAttachmentOutputPresentsExactAuthorizationLink(t *testing.T) {
	t.Parallel()

	output := identityProviderAttachmentOutput(
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

func TestIdentityProviderAttachmentReturnsOnlyBoundedResults(t *testing.T) {
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

			result, ok := identityProviderAttachmentError(test.err)

			require.True(t, ok)
			require.True(t, result.IsError)
			require.Len(t, result.Content, 1)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			require.JSONEq(t, `{"code":"`+test.code+`","message":"`+test.message+`"}`, text.Text)
		})
	}

	result, ok := identityProviderAttachmentError(ErrIdentityProviderAttachmentUnavailable)
	require.False(t, ok)
	require.Nil(t, result)
}
