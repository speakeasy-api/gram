package remotesessions_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func connectionScopesFor(t *testing.T, env syntheticExpiryEnv, resourceURL string) *remotesessions.ScopeResolution {
	t.Helper()
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

// A client that does not own the server's resource is decided by its issuer
// at login, and so by the resolver.
func TestConnectionScopes_MatchInteractiveLoginWithoutOwnership(t *testing.T) {
	t.Parallel()
	resource, _ := fakeResource(t, []string{"files:read"})
	ctx, env := newSyntheticExpiryEnv(t, "parity-unowned", scopelessToken, withIssuerScopes("admin", "openid"), withRemoteServer(resource.URL), withProber())
	q := repo.New(env.db)
	login := env.session.UserSessionIssuerID

	// Serving a second upstream makes the client's own derivation ambiguous,
	// and a sibling bound to the same login then holds the resource.
	other := createUserSessionIssuer(t, ctx, env.db, "parity-unowned-other")
	seedRemoteMcpServerForLogin(t, ctx, env.db, env.projectID, other, "parity-unowned-other", "https://other.example.com/mcp")
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: env.clientID, UserSessionIssuerID: other}))
	sibling := seedProjectRemoteClientNoOrg(t, ctx, env.db, env.projectID, env.issuerID, "parity-unowned-sibling")
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: sibling, UserSessionIssuerID: login}))

	applies, err := env.mgr.ResourceAppliesToClient(ctx, env.organizationID, env.clientID, []uuid.UUID{env.clientID, sibling}, resource.URL)
	require.NoError(t, err)
	require.False(t, applies)

	clients, err := env.mgr.ListClients(ctx, env.projectID, env.organizationID, login)
	require.NoError(t, err)
	var client remotesessions.Client
	for _, c := range clients {
		if c.ID == env.clientID {
			client = c
		}
	}
	require.Equal(t, env.clientID, client.ID)
	subject := urn.NewUserSubject("parity-unowned-subject")
	authURL, err := env.mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID: uuid.NewString(), ProjectID: env.projectID, OrganizationID: env.organizationID, UserSessionIssuerID: login,
		Subject: &subject, McpSlug: "synthetic-mcp-parity-unowned", McpServerID: env.mcpServerID, FinalRedirectURI: "", Resource: "",
	}, client)
	require.NoError(t, err)

	resolution := connectionScopesFor(t, env, resource.URL)
	require.Equal(t, remotesessionmetrics.ScopeSourceIssuerCatalogue, resolution.Source)
	require.Equal(t, scopeOf(t, authURL), strings.Join(resolution.Scopes, " "))
}
