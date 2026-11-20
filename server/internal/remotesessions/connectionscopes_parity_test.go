package remotesessions_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func connectionScopesFor(t *testing.T, env syntheticExpiryEnv, resourceURL string) *remotesessions.ScopeResolution {
	t.Helper()
	offerRegistration(t, t.Context(), env.db, env.organizationID, env.projectID, env.mcpServerID.UUID, env.clientID)
	resolution, err := remotesessions.ConnectionScopes(t.Context(), env.db, remotesessions.BindingConnection{
		ProjectID: env.projectID, OrganizationID: env.organizationID, UserSessionIssuerID: env.session.UserSessionIssuerID,
		ClientID: env.clientID, Resource: resourceURL, Discover: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resolution)
	return resolution
}

// Turn on records what an interactive sign-in through the same client asks
// for, so the resolver must agree with the authorize URL the login minted.
func TestConnectionScopes_MatchInteractiveLogin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		opts []syntheticLoginOption
	}{
		{name: "pin", opts: []syntheticLoginOption{withProtectedResource(protectedResourceSeed{pin: []string{"files:read", "legacy:all"}, fetchedAgo: 2 * time.Hour})}},
		{name: "challenge", opts: []syntheticLoginOption{withProtectedResource(protectedResourceSeed{challengeScopes: []string{"files:read"}, fetchedAgo: 2 * time.Hour})}},
		{name: "advertised", opts: nil},
		{name: "client scope", opts: []syntheticLoginOption{withClientScope("channels:history"), withProtectedResource(protectedResourceSeed{pin: []string{"files:read"}, fetchedAgo: 2 * time.Hour})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resource, _ := fakeResource(t, []string{"files:read", "files:write", "openid"})
			opts := append([]syntheticLoginOption{withIssuerScopes("admin", "openid"), withRemoteServer(resource.URL), withProber()}, tc.opts...)
			_, env := newSyntheticExpiryEnv(t, "parity-"+strings.ReplaceAll(tc.name, " ", "-"), scopelessToken, opts...)
			require.Equal(t, scopeOf(t, env.authURL), strings.Join(connectionScopesFor(t, env, resource.URL).Scopes, " "))
		})
	}
}

// A stale compatibility pair cannot decide connection scope, even when the
// same registration has a genuine offer for a sibling resource.
func TestConnectionScopes_MatchesTheOfferedClientAndExactResource(t *testing.T) {
	t.Parallel()
	resource, _ := fakeResource(t, []string{"files:read"})
	ctx, env := newSyntheticExpiryEnv(t, "parity-unowned", scopelessToken, withIssuerScopes("admin", "openid"), withRemoteServer(resource.URL), withProtectedResource(protectedResourceSeed{pin: []string{"files:read"}}))
	login := env.session.UserSessionIssuerID
	sibling := seedRemoteMcpServerForLogin(t, ctx, env.db, env.projectID, login, "parity-sibling", "https://other.example.com/mcp")
	offerRegistration(t, ctx, env.db, env.organizationID, env.projectID, sibling, env.clientID)
	b := remotesessions.BindingConnection{ProjectID: env.projectID, OrganizationID: env.organizationID, UserSessionIssuerID: login, ClientID: env.clientID, Resource: resource.URL, Discover: true}
	stale, err := remotesessions.ConnectionScopes(ctx, env.db, b)
	require.NoError(t, err)
	require.Nil(t, stale)

	offerRegistration(t, ctx, env.db, env.organizationID, env.projectID, env.mcpServerID.UUID, env.clientID)
	b.MCPServerID = sibling
	wrongTarget, err := remotesessions.ConnectionScopes(ctx, env.db, b)
	require.NoError(t, err)
	require.Nil(t, wrongTarget)
	b.MCPServerID = env.mcpServerID.UUID
	live, err := remotesessions.ConnectionScopes(ctx, env.db, b)
	require.NoError(t, err)
	require.NotNil(t, live)
	require.Equal(t, []string{"files:read"}, live.Scopes)

	b.Discover = false
	withoutDiscovery, err := remotesessions.ConnectionScopes(ctx, env.db, b)
	require.NoError(t, err)
	require.NotNil(t, withoutDiscovery, "the offer still qualifies without discovery")
	require.NotContains(t, withoutDiscovery.Scopes, "files:read", "the resource pin must not affect scopes when discovery is disabled")
}
