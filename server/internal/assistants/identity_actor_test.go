package assistants

import (
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIdentityProvisioningRequiresValidatedHumanOrPlatformOAuth(t *testing.T) {
	t.Parallel()
	session := "session-test"
	actor := &contextvalues.AuthContext{UserID: "user-test", SessionID: &session}
	unvalidated := contextvalues.SetAuthContext(t.Context(), actor)
	require.Error(t, requireAssistantProvisioningActor(unvalidated))
	require.NoError(t, requireAssistantProvisioningActor(contextvalues.WithValidatedGramSession(t.Context(), actor, false)))
	actor.SessionID = nil
	require.Error(t, requireAssistantProvisioningActor(contextvalues.WithValidatedGramSession(t.Context(), actor, false)))
	oauth := contextvalues.SetOAuthClientID(contextvalues.SetAuthContext(t.Context(), actor), "client-test")
	require.Error(t, requireAssistantProvisioningActor(oauth))
	require.NoError(t, requireAssistantProvisioningActor(contextvalues.SetActingSurface(oauth, contextvalues.ActingSurfacePlatformMCP)))
	actor.APIKeyID = "key-test"
	require.Error(t, requireAssistantProvisioningActor(contextvalues.SetActingSurface(oauth, contextvalues.ActingSurfacePlatformMCP)))
}
