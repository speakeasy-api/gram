package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// TestContextWithPrincipal_MarksActingSurface pins the attribution a write made
// under this principal will carry. Binding the principal is the only place
// Platform MCP declares its surface, so a call that reached a tool handler
// without one would be audited as though it came from somewhere else.
func TestContextWithPrincipal_MarksActingSurface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		principal Principal
		surface   ActingSurface
	}{
		{
			name:      "an unset surface is the external endpoint",
			principal: Principal{UserID: "user_test", OrganizationID: "org_test", ConnectionID: "conn", Generation: "gen", ClientID: "client_registered", Surface: ""},
			surface:   SurfacePlatformMCP,
		},
		{
			name:      "the assistant declares its own",
			principal: Principal{UserID: "user_test", OrganizationID: "org_test", ConnectionID: "", Generation: "", ClientID: AssistantClientID, Surface: SurfaceProjectAssistant},
			surface:   SurfaceProjectAssistant,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			marked, ok := contextvalues.GetActingSurface(contextWithPrincipal(t.Context(), tt.principal))

			require.True(t, ok)
			require.Equal(t, string(tt.surface), marked)
		})
	}
}

// TestContextWithPrincipal_ClientIDFollowsTheConnection covers the case that
// makes clearing necessary rather than merely tidy: the assistant adapter binds
// its principal onto a context that already authenticated as some other OAuth
// client. Left alone, the audit trail would name a client that had no part in
// the write.
func TestContextWithPrincipal_ClientIDFollowsTheConnection(t *testing.T) {
	t.Parallel()

	t.Run("a connection names its registered client", func(t *testing.T) {
		t.Parallel()

		ctx := contextWithPrincipal(t.Context(), Principal{
			UserID: "user_test", OrganizationID: "org_test",
			ConnectionID: "conn", Generation: "gen",
			ClientID: "client_registered", Surface: SurfacePlatformMCP,
		})

		clientID, ok := contextvalues.GetOAuthClientID(ctx)

		require.True(t, ok)
		require.Equal(t, "client_registered", clientID)
	})

	t.Run("a connection-less principal does not inherit one", func(t *testing.T) {
		t.Parallel()

		inherited := contextvalues.SetOAuthClientID(t.Context(), "client_from_another_request")

		ctx := contextWithPrincipal(inherited, Principal{
			UserID: "user_test", OrganizationID: "org_test",
			ConnectionID: "", Generation: "",
			ClientID: AssistantClientID, Surface: SurfaceProjectAssistant,
		})

		_, ok := contextvalues.GetOAuthClientID(ctx)

		require.False(t, ok)
	})
}
