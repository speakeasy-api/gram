package mcpidentity_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
)

func TestFromContext_AbsentMeansUnattributed(t *testing.T) {
	t.Parallel()

	identity, ok := mcpidentity.FromContext(t.Context())
	require.False(t, ok)
	require.Empty(t, identity.Kind)
	require.Empty(t, identity.UserID)
}

func TestWithIdentity_RoundTripsAuthenticatedUser(t *testing.T) {
	t.Parallel()

	ctx := mcpidentity.WithIdentity(t.Context(), mcpidentity.AuthenticatedUser("user_01J8EXAMPLE"))
	identity, ok := mcpidentity.FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, mcpidentity.KindUserSession, identity.Kind)
	require.Equal(t, "user_01J8EXAMPLE", identity.UserID)
}

func TestWithIdentity_LatestStampWins(t *testing.T) {
	t.Parallel()

	ctx := mcpidentity.WithIdentity(t.Context(), mcpidentity.AuthenticatedUser("user_01J8EXAMPLE"))
	ctx = mcpidentity.WithIdentity(ctx, mcpidentity.Identity{Kind: mcpidentity.KindAssistant, UserID: ""})

	identity, ok := mcpidentity.FromContext(ctx)
	require.True(t, ok)
	require.Equal(t, mcpidentity.KindAssistant, identity.Kind)
	require.Empty(t, identity.UserID)
}
