package gram

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/marketplace"
)

func TestPlatformMCPLocalFixtureConfigIsEnabledByDefaultLocally(t *testing.T) {
	t.Parallel()

	fixture, err := platformMCPLocalFixtureConfigFromCLI("local", "https://localhost:8080")
	require.NoError(t, err)
	require.NotNil(t, fixture)
	require.NotNil(t, fixture.Fixture)
	require.Equal(t, "https://localhost:8080", fixture.Origin.String())
}

func TestPlatformMCPLocalFixtureConfigAllowsLocalHTTPWithoutSyntheticSource(t *testing.T) {
	t.Parallel()

	fixture, err := platformMCPLocalFixtureConfigFromCLI("local", "http://localhost:8080")
	require.NoError(t, err)
	require.Nil(t, fixture)
}

func TestPlatformMCPLocalFixtureConfigDoesNotLeakOutsideLocal(t *testing.T) {
	t.Parallel()

	fixture, err := platformMCPLocalFixtureConfigFromCLI("production", "https://localhost:8080")
	require.NoError(t, err)
	require.Nil(t, fixture)
}

func TestLocalPlatformMCPMarketplaceTokenResolvesDedicatedRepository(t *testing.T) {
	t.Parallel()

	require.Len(t, localPlatformMCPMarketplaceToken, 43)
	require.NotContains(t, localPlatformMCPMarketplaceToken, ".")

	resolver := localMarketplaceResolver{projectRepositories: rejectingMarketplaceResolver{}}
	upstream, err := resolver.Resolve(t.Context(), localPlatformMCPMarketplaceToken)
	require.NoError(t, err)
	require.Equal(t, localPlatformMCPMarketplaceOwner, upstream.Owner)
	require.Equal(t, localPlatformMCPMarketplaceRepo, upstream.Repo)
	require.True(t, strings.HasPrefix(localPlatformMCPMarketplaceOwner, "local-platform-mcp"))

	require.Equal(
		t,
		"https://localhost:8080/marketplace/"+localPlatformMCPMarketplaceToken+".git",
		localPlatformMCPMarketplaceURL("https://localhost:8080/"),
	)
	require.True(t, isLocalPlatformMCPMarketplaceRoute(httptest.NewRequest(
		"GET",
		"/marketplace/"+localPlatformMCPMarketplaceToken+".git/info/refs",
		nil,
	)))
	require.False(t, isLocalPlatformMCPMarketplaceRoute(httptest.NewRequest(
		"GET",
		"/marketplace/"+strings.Repeat("a", 43)+".git/info/refs",
		nil,
	)))
}

type rejectingMarketplaceResolver struct{}

func (rejectingMarketplaceResolver) Resolve(_ context.Context, _ string) (marketplace.Upstream, error) {
	return marketplace.Upstream{}, marketplace.ErrNotFound
}
