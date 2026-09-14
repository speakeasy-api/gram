package gram

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/marketplace"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

type allowingPlatformMCPBudget struct{}

func (allowingPlatformMCPBudget) Allow(context.Context, string) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: true}, nil
}
func (allowingPlatformMCPBudget) AllowN(context.Context, string, int) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: true}, nil
}

// The Shadow AI reads hang off the same attach, and both halves are held to
// a live org:admin recheck rather than the session that installed the package.
type stubLiveOrgAdminAuthorizer struct{}

func (stubLiveOrgAdminAuthorizer) RequireLiveOrgAdmin(context.Context, platformmcp.Principal) error {
	return nil
}

func TestAttachShadowInventoryConstructsWithLocalFixtureDependencies(t *testing.T) {
	t.Parallel()

	limiter := allowingPlatformMCPBudget{}
	reader := platformmcp.NewPostgresReader(testenv.NewLogger(t), nil)
	attached := attachShadowInventory(reader, platformMCPConfig{
		DB: nil, JWTSigningKey: "test-signing-key", FeatureFlags: &feature.InMemory{},
		ShadowInventory: &access.Service{}, ShadowReview: &mcpapproval.Service{},
	}, stubLiveOrgAdminAuthorizer{}, platformmcp.OperationBudget{Connection: limiter, Organization: limiter})
	require.True(t, attached)
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
