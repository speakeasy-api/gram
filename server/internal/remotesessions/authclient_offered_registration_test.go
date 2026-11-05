package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// offerRegistration has serverID offer registrationID through a personal auth
// client an administrator added. It writes no user session issuer link.
func offerRegistration(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID, serverID, registrationID uuid.UUID) uuid.UUID {
	t.Helper()
	q := authclientbackfill.New(conn)
	authClientID, err := q.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{
		OrganizationID:        organizationID,
		ProjectID:             conv.ToNullUUID(projectID),
		ClientType:            authclientbackfill.TypeRemoteSession,
		Source:                "admin",
		RemoteSessionClientID: conv.ToNullUUID(registrationID),
	})
	require.NoError(t, err)
	require.NoError(t, q.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{
		ProjectID:      projectID,
		OrganizationID: organizationID,
		McpServerID:    serverID,
		AuthClientID:   authClientID,
		Source:         "admin",
	}))
	return authClientID
}

// A registration an administrator adds to a server as a personal auth client
// has no user session issuer link. Its connect must still name the server's
// upstream as the resource, which only the offer can supply.
func TestResourceForClientAtUpstream_OfferedRegistrationWithoutASignInLink(t *testing.T) {
	t.Parallel()
	const upstream = "https://offered.example.com/mcp"
	for _, tc := range []struct {
		name     string
		enforced bool
		enabled  bool
		want     string
	}{
		{name: "enforced", enforced: true, enabled: true, want: upstream},
		{name: "not enforced", enforced: false, enabled: true, want: ""},
		{name: "enforced with the auth client disabled", enforced: true, enabled: false, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.ProjectID)
			projectID, organizationID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

			signIn := createUserSessionIssuer(t, ctx, ti.conn, "usi-offered-resource")
			serverID := seedRemoteMcpServerForLogin(t, ctx, ti.conn, projectID, signIn, "offered-resource", upstream)
			issuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(organizationID), "offered-resource-issuer", "https://issuer.example.com/offered-resource")
			registration := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, projectID, issuerID, "offered-resource-cid")
			authClientID := offerRegistration(t, ctx, ti.conn, organizationID, projectID, serverID, registration)
			if !tc.enabled {
				require.NoError(t, authclientbackfill.New(ti.conn).SetAuthClientEnabledFixture(ctx, authclientbackfill.SetAuthClientEnabledFixtureParams{Enabled: false, ID: authClientID}))
			}
			if tc.enforced {
				enforceUpstreamAuth(t, ctx, ti.conn, organizationID)
			}

			mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
			got, err := mgr.ResourceForClientAtUpstream(ctx, organizationID, registration, nil, upstream)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)

			applies, err := mgr.ResourceAppliesToClient(ctx, organizationID, registration, nil, upstream)
			require.NoError(t, err)
			require.Equal(t, tc.want != "", applies, "the server's resource scopes apply to a login exactly when its grant is qualified to that server")
		})
	}
}

// A tunneled server has no URL to name, but its recorded resource identifier is
// the audience its provider expects. Offered with no sign-in link, the
// registration's login names that identifier, and a tunnel that records none
// connects unqualified.
func TestResourceForClientAtUpstream_OfferedTunneledServer(t *testing.T) {
	t.Parallel()
	const identifier = "https://tunnel.internal.example.com/mcp"
	for _, tc := range []struct {
		name       string
		identifier string
	}{
		{name: "records an identifier", identifier: identifier},
		{name: "records none", identifier: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.ProjectID)
			projectID, organizationID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

			signIn := createUserSessionIssuer(t, ctx, ti.conn, "usi-offered-tunnel")
			serverID := seedTunneledMCPServerForIssuer(t, ctx, ti, signIn, "offered-tunnel", tc.identifier)
			issuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(organizationID), "offered-tunnel-issuer", "https://issuer.example.com/offered-tunnel")
			registration := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, projectID, issuerID, "offered-tunnel-cid")
			offerRegistration(t, ctx, ti.conn, organizationID, projectID, serverID, registration)
			enforceUpstreamAuth(t, ctx, ti.conn, organizationID)

			got, err := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t)).ResourceForClientAtUpstream(ctx, organizationID, registration, nil, tc.identifier)
			require.NoError(t, err)
			require.Equal(t, tc.identifier, got)
		})
	}
}

// An administrator rotating or deleting a client is told which servers it
// affects. A server that offers it with no sign-in link is one of them.
func TestClientMcpServers_IncludeOfferingServersWithoutASignInLink(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID, organizationID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	signIn := createUserSessionIssuer(t, ctx, ti.conn, "usi-offered-impact")
	serverID := seedRemoteMcpServerForLogin(t, ctx, ti.conn, projectID, signIn, "offered-impact", "https://impact.example.com/mcp")
	issuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(organizationID), "offered-impact-issuer", "https://issuer.example.com/offered-impact")
	registration := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, projectID, issuerID, "offered-impact-cid")
	offerRegistration(t, ctx, ti.conn, organizationID, projectID, serverID, registration)
	enforceUpstreamAuth(t, ctx, ti.conn, organizationID)

	preflight, err := ti.service.GetClientDeletePreflight(ctx, &orgclientsgen.GetClientDeletePreflightPayload{ID: registration.String()})
	require.NoError(t, err)
	require.Equal(t, []string{"offered-impact"}, preflight.McpServerNames)

	servers, err := ti.service.ListClientMcpServers(ctx, &orgclientsgen.ListClientMcpServersPayload{ClientID: registration.String()})
	require.NoError(t, err)
	require.Len(t, servers.Items, 1)
	require.Equal(t, serverID.String(), servers.Items[0].ID)
}

// A sign-in can front servers with different upstreams. Where typed auth
// clients are enforced, the server that offers the registration decides its
// resource, not every server behind the sign-in it is linked to.
func TestResourceForClientAtUpstream_OfferDecidesOverSignInLinks(t *testing.T) {
	t.Parallel()
	const offering, sibling = "https://offering.example.com/mcp", "https://sibling.example.com/mcp"
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID, organizationID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	signIn := createUserSessionIssuer(t, ctx, ti.conn, "usi-offer-decides")
	offeringServer := seedRemoteMcpServerForLogin(t, ctx, ti.conn, projectID, signIn, "offer-decides-a", offering)
	seedRemoteMcpServerForLogin(t, ctx, ti.conn, projectID, signIn, "offer-decides-b", sibling)
	issuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(organizationID), "offer-decides-issuer", "https://issuer.example.com/offer-decides")
	registration := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, projectID, issuerID, "offer-decides-cid")
	require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: registration,
		UserSessionIssuerID:   signIn,
	}))
	offerRegistration(t, ctx, ti.conn, organizationID, projectID, offeringServer, registration)
	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))

	got, err := mgr.ResourceForClientAtUpstream(ctx, organizationID, registration, nil, sibling)
	require.NoError(t, err)
	require.Equal(t, sibling, got, "without enforcement the sign-in's servers decide, and the registration claims the upstream it is connected through")

	enforceUpstreamAuth(t, ctx, ti.conn, organizationID)

	got, err = mgr.ResourceForClientAtUpstream(ctx, organizationID, registration, nil, sibling)
	require.NoError(t, err)
	require.Equal(t, offering, got, "under enforcement only the server offering the registration decides")
}

// Disconnecting a registration a server offers must end the grant and tell the
// provider, with no user session issuer link to reach it through.
func TestDisconnectAuthClientRemoteSession_RevokesUpstreamWithoutASignInLink(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	upstream := newRevocationUpstream(t, spy)
	fx := seedRevocableSession(t, ctx, ti, "offered-disconnect", upstream.URL+"/revoke", "s3cret", true)
	require.NoError(t, repo.New(ti.conn).DeleteUserSessionIssuerAttachmentsForRemoteSessionClient(ctx, repo.DeleteUserSessionIssuerAttachmentsForRemoteSessionClientParams{
		RemoteSessionClientID: fx.clientID,
		ProjectID:             conv.ToNullUUID(fx.projectID),
	}))

	n, err := newDisconnectChallengeManager(t, ti).DisconnectAuthClientRemoteSession(ctx, fx.subject, fx.projectID, fx.organizationID, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	calls, form, _ := spy.snapshot()
	require.Equal(t, 1, calls, "exactly one RFC 7009 request")
	require.Equal(t, fx.refreshToken, form.Get("token"))
	require.Equal(t, fx.externalCID, form.Get("client_id"))
	requireSessionRevoked(t, ctx, ti, fx)
}

// The offer authorizes the registration, not the tenant: a project the
// registration does not reach, and a subject who does not hold the grant,
// change nothing.
func TestAuthClientRemoteSessionActions_StayInTheTenant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	fx := seedRevocableSession(t, ctx, ti, "offered-tenant", "", "s3cret", true)
	otherProject := createProject(t, ctx, ti.conn, "offered-tenant-other")
	otherSubject := urn.NewUserSubject("offered-tenant-other-user")
	mgr := newDisconnectChallengeManager(t, ti)

	// Both callers share the one grant, so they run in turn rather than as
	// parallel subtests.
	requireOutOfReach := func(why string, subject urn.SessionSubject, projectID uuid.UUID) {
		n, err := mgr.DisconnectAuthClientRemoteSession(ctx, subject, projectID, fx.organizationID, fx.clientID)
		require.NoError(t, err, why)
		require.Zero(t, n, why)

		n, err = mgr.SetAuthClientRemoteSessionAutoRefresh(ctx, subject, projectID, fx.organizationID, fx.clientID, true)
		require.NoError(t, err, why)
		require.Zero(t, n, why)

		_, err = mgr.RefreshAuthClientRemoteSession(ctx, remotesessions.RefreshAttribution{}, subject, projectID, fx.organizationID, fx.clientID)
		require.ErrorIs(t, err, remotesessions.ErrRemoteSessionNotRefreshable, why)
	}
	requireOutOfReach("another project", fx.subject, otherProject)
	requireOutOfReach("another subject", otherSubject, fx.projectID)

	_, err := mgr.DisconnectAuthClientRemoteSession(ctx, fx.subject, uuid.Nil, fx.organizationID, fx.clientID)
	require.ErrorIs(t, err, remotesessions.ErrInvalidAuthorizationRequest, "a request naming no project is refused rather than matching nothing")

	session, err := repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: fx.subject, RemoteSessionClientID: fx.clientID})
	require.NoError(t, err, "the grant is untouched")
	require.False(t, session.AutoRefresh)
}

// A manual refresh and the auto-refresh choice reach a registration a server
// offers through the offer alone.
func TestRefreshAuthClientRemoteSession_RefreshesWithoutASignInLink(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID, organizationID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	upstream := newRefreshTokenUpstream(t, "offered-refreshed-access")
	issuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(organizationID), "offered-refresh-issuer", upstream.URL)
	signIn := createUserSessionIssuer(t, ctx, ti.conn, "usi-offered-refresh")
	registration := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, projectID, issuerID, "offered-refresh-cid")
	subject := seedRefreshableSharedSession(t, ctx, ti, signIn, registration, "offered-refresh")
	mgr := newDisconnectChallengeManager(t, ti)

	result, err := mgr.RefreshAuthClientRemoteSession(ctx, remotesessions.RefreshAttribution{}, subject, projectID, organizationID, registration)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeRefreshed, result.Outcome)
	require.Equal(t, "offered-refreshed-access", result.AccessToken)

	stored, err := repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: registration})
	require.NoError(t, err)
	plain, err := testenv.NewEncryptionClient(t).Decrypt(stored.AccessTokenEncrypted)
	require.NoError(t, err)
	require.Equal(t, "offered-refreshed-access", plain, "the rotated token is persisted on the grant")

	n, err := mgr.SetAuthClientRemoteSessionAutoRefresh(ctx, subject, projectID, organizationID, registration, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	statuses, err := mgr.AuthClientRemoteSessionStatuses(ctx, subject, projectID, organizationID, []uuid.UUID{registration})
	require.NoError(t, err)
	require.True(t, statuses[registration].AutoRefresh)
}
