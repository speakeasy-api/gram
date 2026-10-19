package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// A failure to evaluate grants is not a denial: discovery must fail rather
// than read as "no tools".
func TestAuthorizedDiscoveryToolset_PropagatesAuthorizationErrors(t *testing.T) {
	t.Parallel()

	engine := authz.NewEngine(testenv.NewLogger(t), nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	sessionID := "session_discovery"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org_discovery",
		UserID:               "user_discovery",
		SessionID:            &sessionID,
	})
	// No grants were prepared on this context, so evaluation itself fails.
	payload := &mcpInputs{authenticated: true, projectID: uuid.New()}
	toolset := &types.Toolset{
		ID: uuid.NewString(),
		Tools: []*types.Tool{{
			HTTPToolDefinition: &types.HTTPToolDefinition{Name: "reader", Description: "Reads"},
		}},
	}

	_, _, _, err := authorizedDiscoveryToolset(ctx, engine, payload, toolset)
	require.Error(t, err)
}

// Without per-tool enforcement the toolset is discovered as it is.
func TestAuthorizedDiscoveryToolset_PublicServerIsUnrestricted(t *testing.T) {
	t.Parallel()

	engine := authz.NewEngine(testenv.NewLogger(t), nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	public := true
	payload := &mcpInputs{authenticated: true, projectID: uuid.New()}
	toolset := &types.Toolset{
		ID:          uuid.NewString(),
		McpIsPublic: &public,
		Tools: []*types.Tool{{
			HTTPToolDefinition: &types.HTTPToolDefinition{Name: "reader", Description: "Reads"},
		}},
	}

	discovery, allowed, restricted, err := authorizedDiscoveryToolset(t.Context(), engine, payload, toolset)
	require.NoError(t, err)
	require.Same(t, toolset, discovery)
	require.Equal(t, 1, allowed)
	require.False(t, restricted)
}
