package feature_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

func TestAuthClientResolverMode(t *testing.T) {
	t.Parallel()

	flags := new(feature.InMemory)
	require.Equal(t, feature.AuthClientResolverOff, feature.AuthClientResolver(t.Context(), flags, "org_a"))
	require.Equal(t, feature.AuthClientResolverOff, feature.AuthClientResolver(t.Context(), nil, "org_a"))

	flags.SetFlag(feature.FlagAuthClientResolverShadow, "org_a", true)
	require.Equal(t, feature.AuthClientResolverShadow, feature.AuthClientResolver(t.Context(), flags, "org_a"))
	require.Equal(t, feature.AuthClientResolverOff, feature.AuthClientResolver(t.Context(), flags, "org_b"))

	flags.SetFlag(feature.FlagAuthClientResolver, "org_a", true)
	require.Equal(t, feature.AuthClientResolverEnforced, feature.AuthClientResolver(t.Context(), flags, "org_a"), "enforcement wins over shadow")
	require.Equal(t, feature.AuthClientResolverOff, feature.AuthClientResolver(t.Context(), flags, ""))
}
