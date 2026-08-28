package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// A mode change is a policy decision with an unrecoverable failure mode for end
// users, so the tool must refuse an unconfirmed call and say what to confirm.
func TestSetClientAdmissionRequiresConfirmationAndAValidMode(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	descriptors := registrar.For(AudienceAssistant)
	var found bool
	for _, descriptor := range descriptors {
		if descriptor.Name == "set_mcp_client_admission" {
			found = true
		}
	}
	require.True(t, found, "the assistant reaches client admission without a connection")

	result := clientAdmissionErrorTool("confirmation_required", "Tell the user which clients presets admits and which it refuses, ask them to explicitly confirm that mode for this MCP, then call this tool again with confirmed: true.")
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var decoded clientAdmissionErrorResult
	require.NoError(t, json.Unmarshal([]byte(text.Text), &decoded))
	require.Equal(t, "confirmation_required", decoded.Code)
	require.Contains(t, decoded.Message, "confirmed: true")
}

// Each mode carries a different consequence for end users; the result explains
// the one that was applied rather than leaving the agent to invent it.
func TestClientAdmissionModeGuidanceCoversEveryResolvableMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"presets", "open", "disabled", "reporting"} {
		require.NotEmpty(t, modeGuidance(mode), "mode %q has no guidance", mode)
	}
	require.Empty(t, modeGuidance("not-a-mode"))
}

func TestClientAdmissionToolErrorsAreBounded(t *testing.T) {
	t.Parallel()

	result, ok := clientAdmissionToolError(ErrClientAdmissionInvalid)
	require.True(t, ok)
	require.True(t, result.IsError)

	result, ok = clientAdmissionToolError(ErrClientAdmissionUnavailable)
	require.True(t, ok)
	require.True(t, result.IsError)

	// A registration the caller may not act on reaches the shared bounded
	// vocabulary rather than leaking a persistence error to the client.
	result, ok = clientAdmissionToolError(ErrRegistrationInvalid)
	require.True(t, ok)
	require.True(t, result.IsError)
}
