package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// offeredRegistration is a registration a server offers through an auth client,
// with no user session issuer link. The server's sign-in is the one a person
// holds a session on.
type offeredRegistration struct {
	ti           *testInstance
	projectID    uuid.UUID
	organization string
	signIn       uuid.UUID
	serverID     uuid.UUID
	upstream     string
	issuerID     uuid.UUID
	registration uuid.UUID
	authClientID uuid.UUID
	subject      urn.SessionSubject
}

func seedOfferedRegistration(t *testing.T, slug, issuerURL string) (context.Context, offeredRegistration) {
	t.Helper()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID, organizationID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	signIn := createUserSessionIssuer(t, ctx, ti.conn, slug+"-usi")
	upstream := "https://" + slug + ".example.com/mcp"
	serverID := seedRemoteMcpServerForLogin(t, ctx, ti.conn, projectID, signIn, slug, upstream)
	issuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(organizationID), slug+"-issuer", issuerURL)
	registration := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, projectID, issuerID, slug+"-cid")
	authClientID := offerRegistration(t, ctx, ti.conn, organizationID, projectID, serverID, registration)
	return ctx, offeredRegistration{
		ti: ti, projectID: projectID, organization: organizationID, signIn: signIn, serverID: serverID, upstream: upstream,
		issuerID: issuerID, registration: registration, authClientID: authClientID, subject: urn.NewUserSubject("subject-" + slug),
	}
}

// grant stores the subject's grant for the registration, consented through
// mintedBy, shaped for the refresh sweep when it holds a refresh token and for
// the idle re-check when it does not, and old enough for either.
func (o offeredRegistration) grant(t *testing.T, ctx context.Context, mintedBy uuid.UUID, renewable, autoRefresh bool) uuid.UUID {
	t.Helper()
	var refreshToken pgtype.Text
	if renewable {
		refreshToken = conv.ToPGText("refresh-ciphertext")
	}
	session, err := repo.New(o.ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            o.subject,
		UserSessionIssuerID:   mintedBy,
		RemoteSessionClientID: o.registration,
		AccessTokenEncrypted:  "access-ciphertext",
		AccessExpiresAt:       pgtype.Timestamptz{},
		RefreshTokenEncrypted: refreshToken,
		RefreshExpiresAt:      pgtype.Timestamptz{},
		Scopes:                []string{},
		Resource:              pgtype.Text{},
		AutoRefresh:           autoRefresh,
	})
	require.NoError(t, err)
	require.NoError(t, repo.New(o.ti.conn).SetRemoteSessionUpdatedAt(ctx, repo.SetRemoteSessionUpdatedAtParams{
		ID:        session.ID,
		ProjectID: conv.ToNullUUID(o.projectID),
		UpdatedAt: conv.ToPGTimestamptz(time.Now().Add(-25 * time.Hour)),
	}))
	require.NoError(t, testrepo.New(o.ti.conn).SetRemoteSessionValidationTrackingFixture(ctx, testrepo.SetRemoteSessionValidationTrackingFixtureParams{
		LastValidatedAt:      pgtype.Timestamptz{},
		LastRefreshAttemptAt: pgtype.Timestamptz{},
		CreatedAt:            conv.ToPGTimestamptz(time.Now().Add(-(recheckTestInterval + time.Hour))),
		ID:                   session.ID,
		ProjectID:            conv.ToNullUUID(o.projectID),
	}))
	return session.ID
}

func (o offeredRegistration) requireNoLink(t *testing.T, ctx context.Context) {
	t.Helper()
	linked, err := repo.New(o.ti.conn).CheckRemoteSessionClientBindingForUserSessionIssuer(ctx, repo.CheckRemoteSessionClientBindingForUserSessionIssuerParams{
		RemoteSessionClientID: o.registration,
		UserSessionIssuerID:   o.signIn,
		ProjectID:             o.projectID,
		OrganizationID:        o.organization,
	})
	require.NoError(t, err)
	require.False(t, linked, "the sweep never writes a user session issuer link")
}

// offeredReachCase changes one thing about an otherwise eligible offered
// registration that takes its grant out of the background sweeps.
type offeredReachCase struct {
	name string
	// skipEnforcement leaves the organization without typed enforcement.
	skipEnforcement bool
	// skipSession leaves the subject without a Speakeasy session.
	skipSession bool
	// sessionLapsed gives the subject a Speakeasy session that has lapsed.
	sessionLapsed bool
	breakIt       func(t *testing.T, ctx context.Context, o offeredRegistration)
}

func (c offeredReachCase) sessionLifetime() time.Duration {
	if c.sessionLapsed {
		return -time.Hour
	}
	return 24 * time.Hour
}

func offeredReachCases() []offeredReachCase {
	return []offeredReachCase{
		{name: "typed auth clients are not enforced", skipEnforcement: true},
		{name: "the subject holds no session", skipSession: true},
		{name: "the subject's session has lapsed", sessionLapsed: true},
		{name: "the auth client is disabled", breakIt: func(t *testing.T, ctx context.Context, o offeredRegistration) {
			t.Helper()
			require.NoError(t, authclientbackfill.New(o.ti.conn).SetAuthClientEnabledFixture(ctx, authclientbackfill.SetAuthClientEnabledFixtureParams{Enabled: false, ID: o.authClientID}))
		}},
		{name: "the auth client is deleted", breakIt: func(t *testing.T, ctx context.Context, o offeredRegistration) {
			t.Helper()
			require.NoError(t, authclientbackfill.New(o.ti.conn).SoftDeleteAuthClientFixture(ctx, o.authClientID))
		}},
		{name: "the server no longer offers it", breakIt: func(t *testing.T, ctx context.Context, o offeredRegistration) {
			t.Helper()
			require.NoError(t, authclientbackfill.New(o.ti.conn).SoftDeleteServerAuthClientLinksFixture(ctx, o.serverID))
		}},
		{name: "the offering server is deleted", breakIt: func(t *testing.T, ctx context.Context, o offeredRegistration) {
			t.Helper()
			require.NoError(t, authclientbackfill.New(o.ti.conn).SoftDeleteMCPServerFixture(ctx, o.serverID))
		}},
		{name: "the sign-in is deleted", breakIt: func(t *testing.T, ctx context.Context, o offeredRegistration) {
			t.Helper()
			_, err := testrepo.New(o.ti.conn).SoftDeleteAttachmentConfigFixture(ctx, testrepo.SoftDeleteAttachmentConfigFixtureParams{ID: o.signIn, ProjectID: conv.ToNullUUID(o.projectID)})
			require.NoError(t, err)
		}},
		{name: "the provider is retired", breakIt: func(t *testing.T, ctx context.Context, o offeredRegistration) {
			t.Helper()
			_, err := repo.New(o.ti.conn).SoftDeleteRemoteSessionIssuerFixture(ctx, o.issuerID)
			require.NoError(t, err)
		}},
		{name: "the client is deleted", breakIt: func(t *testing.T, ctx context.Context, o offeredRegistration) {
			t.Helper()
			require.NoError(t, authclientbackfill.New(o.ti.conn).SoftDeleteRemoteSessionClientFixture(ctx, o.registration))
		}},
	}
}

// A grant on an offered registration is refreshed in the background while the
// offer and the subject's session are live, with no link and none written.
func TestRefreshSweep_OfferedRegistrationWithoutASignInLink(t *testing.T) {
	t.Parallel()
	ctx, o := seedOfferedRegistration(t, "sweep-offered", "https://issuer.example.com/sweep-offered")
	enforceUpstreamAuth(t, ctx, o.ti.conn, o.organization)
	enableOrgAutoRefreshFeature(t, ctx, o.ti, o.organization, productfeatures.FeatureRemoteSessionAutoRefresh)
	sessionID := o.grant(t, ctx, o.signIn, true, true)
	seedGramSession(t, ctx, o.ti, o.subject, o.signIn, "sweep-offered", 24*time.Hour)
	window := newSweepWindow()
	q := repo.New(o.ti.conn)

	rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, sessionID, rows[0].ID)
	require.Equal(t, o.organization, rows[0].OrganizationID)

	candidate, err := q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(sessionID, o.organization))
	require.NoError(t, err)
	require.Equal(t, sessionID, candidate.RemoteSession.ID)

	_, err = q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(sessionID, "another-organization"))
	require.ErrorIs(t, err, pgx.ErrNoRows, "the re-check stays in the organization the grant was claimed under")

	rows, err = q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Empty(t, rows, "a claimed grant rotates out for the attempt window")
	o.requireNoLink(t, ctx)
}

func TestRefreshSweep_OfferedRegistrationOutOfReach(t *testing.T) {
	t.Parallel()
	for _, tc := range offeredReachCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, o := seedOfferedRegistration(t, "sweep-out", "https://issuer.example.com/sweep-out")
			if !tc.skipEnforcement {
				enforceUpstreamAuth(t, ctx, o.ti.conn, o.organization)
			}
			enableOrgAutoRefreshFeature(t, ctx, o.ti, o.organization, productfeatures.FeatureRemoteSessionAutoRefreshEnforced)
			sessionID := o.grant(t, ctx, o.signIn, true, true)
			if !tc.skipSession {
				seedGramSession(t, ctx, o.ti, o.subject, o.signIn, "sweep-out", tc.sessionLifetime())
			}
			if tc.breakIt != nil {
				tc.breakIt(t, ctx, o)
			}
			window := newSweepWindow()
			q := repo.New(o.ti.conn)

			rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
			require.NoError(t, err)
			require.Empty(t, rows)

			_, err = q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(sessionID, o.organization))
			require.ErrorIs(t, err, pgx.ErrNoRows, "the claim and its re-check agree")
		})
	}
}

// The organization's automatic-refresh policy and the subject's own choice
// decide an offered registration's sweep as they do a linked one's.
func TestRefreshSweep_OfferedRegistrationFollowsAutoRefreshPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		policy      productfeatures.Feature
		autoRefresh bool
		want        bool
	}{
		{name: "enforced policy overrides an opt-out", policy: productfeatures.FeatureRemoteSessionAutoRefreshEnforced, autoRefresh: false, want: true},
		{name: "opt-in policy with the choice on", policy: productfeatures.FeatureRemoteSessionAutoRefresh, autoRefresh: true, want: true},
		{name: "opt-in policy with the choice off", policy: productfeatures.FeatureRemoteSessionAutoRefresh, autoRefresh: false, want: false},
		{name: "no policy with the choice on", policy: "", autoRefresh: true, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, o := seedOfferedRegistration(t, "sweep-policy", "https://issuer.example.com/sweep-policy")
			enforceUpstreamAuth(t, ctx, o.ti.conn, o.organization)
			if tc.policy != "" {
				enableOrgAutoRefreshFeature(t, ctx, o.ti, o.organization, tc.policy)
			}
			sessionID := o.grant(t, ctx, o.signIn, true, tc.autoRefresh)
			seedGramSession(t, ctx, o.ti, o.subject, o.signIn, "sweep-policy", 24*time.Hour)
			window := newSweepWindow()
			q := repo.New(o.ti.conn)

			rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
			require.NoError(t, err)
			_, recheckErr := q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(sessionID, o.organization))
			if tc.want {
				require.Len(t, rows, 1)
				require.NoError(t, recheckErr)
				return
			}
			require.Empty(t, rows)
			require.ErrorIs(t, recheckErr, pgx.ErrNoRows)
		})
	}
}

// The consent card's auto-refresh choice for an offered registration is what
// the next sweep acts on.
func TestRefreshSweep_OfferedRegistrationHonorsTheCardChoice(t *testing.T) {
	t.Parallel()
	ctx, o := seedOfferedRegistration(t, "sweep-card", "https://issuer.example.com/sweep-card")
	enforceUpstreamAuth(t, ctx, o.ti.conn, o.organization)
	enableOrgAutoRefreshFeature(t, ctx, o.ti, o.organization, productfeatures.FeatureRemoteSessionAutoRefresh)
	sessionID := o.grant(t, ctx, o.signIn, true, false)
	seedGramSession(t, ctx, o.ti, o.subject, o.signIn, "sweep-card", 24*time.Hour)
	window := newSweepWindow()
	q := repo.New(o.ti.conn)
	mgr := newDisconnectChallengeManager(t, o.ti)

	rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Empty(t, rows, "the subject has not chosen auto-refresh")

	n, err := mgr.SetAuthClientRemoteSessionAutoRefresh(ctx, o.subject, o.projectID, o.organization, o.registration, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	rows, err = q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Len(t, rows, 1, "choosing auto-refresh on the card puts the grant in the sweep")
	require.Equal(t, sessionID, rows[0].ID)

	_, err = testrepo.New(o.ti.conn).ClearAttachmentRefreshClaimFixture(ctx, sessionID)
	require.NoError(t, err)
	n, err = mgr.SetAuthClientRemoteSessionAutoRefresh(ctx, o.subject, o.projectID, o.organization, o.registration, false)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	rows, err = q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Empty(t, rows, "turning it off takes the grant back out")
}

// A person who connected through a gateway holds their session on the sign-in
// they consented through, not on the member server's. An organization-level
// registration a member offers is kept alive by that session.
func TestRefreshSweep_OfferedRegistrationConsentedThroughAnotherSignIn(t *testing.T) {
	t.Parallel()
	ctx, o := seedOfferedRegistration(t, "sweep-gateway", "https://issuer.example.com/sweep-gateway")
	enforceUpstreamAuth(t, ctx, o.ti.conn, o.organization)
	enableOrgAutoRefreshFeature(t, ctx, o.ti, o.organization, productfeatures.FeatureRemoteSessionAutoRefreshEnforced)
	issuer := seedOrgLevelRemoteIssuer(t, ctx, o.ti.conn, o.organization, "sweep-gateway-org-issuer")
	o.registration = seedOrgLevelRemoteClient(t, ctx, o.ti.conn, o.organization, issuer, "sweep-gateway-org-cid")
	offerRegistration(t, ctx, o.ti.conn, o.organization, o.projectID, o.serverID, o.registration)
	gateway := seedOrganizationTierUserSessionIssuer(t, ctx, o.ti.conn, "sweep-gateway-usi")
	sessionID := o.grant(t, ctx, gateway, true, false)
	// The grant's age cannot be backdated through an organization-level client,
	// so the window's cutoff moves ahead of it.
	window := newSweepWindow()
	window.keepaliveCutoff = time.Now().Add(time.Hour)
	q := repo.New(o.ti.conn)

	unrelated := createUserSessionIssuer(t, ctx, o.ti.conn, "sweep-gateway-unrelated")
	seedGramSession(t, ctx, o.ti, o.subject, unrelated, "sweep-gateway-unrelated", 24*time.Hour)
	rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Empty(t, rows, "a session on a sign-in neither the offer nor the consent names keeps nothing alive")

	seedGramSession(t, ctx, o.ti, o.subject, gateway, "sweep-gateway", 24*time.Hour)
	rows, err = q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, sessionID, rows[0].ID)
	require.Equal(t, o.organization, rows[0].OrganizationID)
}

// An offer only reaches issuers of the organization it belongs to, and only for
// a grant consented in that organization.
func TestRefreshSweep_OfferedRegistrationStaysInItsOrganization(t *testing.T) {
	t.Parallel()

	t.Run("a grant consented in another organization", func(t *testing.T) {
		t.Parallel()
		ctx, o := seedOfferedRegistration(t, "sweep-foreign-grant", "https://issuer.example.com/sweep-foreign-grant")
		enforceUpstreamAuth(t, ctx, o.ti.conn, o.organization)
		enableOrgAutoRefreshFeature(t, ctx, o.ti, o.organization, productfeatures.FeatureRemoteSessionAutoRefreshEnforced)
		foreign := seedForeignOrganizationSignIn(t, ctx, o.ti, "sweep-foreign-grant")
		sessionID := o.grant(t, ctx, foreign.signIn, true, true)
		seedGramSession(t, ctx, o.ti, o.subject, o.signIn, "sweep-foreign-grant-a", 24*time.Hour)
		seedGramSession(t, ctx, o.ti, o.subject, foreign.signIn, "sweep-foreign-grant-b", 24*time.Hour)
		enableOrgAutoRefreshFeature(t, ctx, o.ti, foreign.organization, productfeatures.FeatureRemoteSessionAutoRefreshEnforced)
		window := newSweepWindow()
		q := repo.New(o.ti.conn)

		rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
		require.NoError(t, err)
		require.Empty(t, rows)
		for _, organization := range []string{o.organization, foreign.organization} {
			_, err = q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(sessionID, organization))
			require.ErrorIs(t, err, pgx.ErrNoRows, organization)
		}
	})

	t.Run("an offering server behind another organization's sign-in", func(t *testing.T) {
		t.Parallel()
		ctx, o := seedOfferedRegistration(t, "sweep-foreign-server", "https://issuer.example.com/sweep-foreign-server")
		enforceUpstreamAuth(t, ctx, o.ti.conn, o.organization)
		enableOrgAutoRefreshFeature(t, ctx, o.ti, o.organization, productfeatures.FeatureRemoteSessionAutoRefreshEnforced)
		issuer := seedOrgLevelRemoteIssuer(t, ctx, o.ti.conn, o.organization, "sweep-foreign-server-org-issuer")
		o.registration = seedOrgLevelRemoteClient(t, ctx, o.ti.conn, o.organization, issuer, "sweep-foreign-server-org-cid")
		foreign := seedForeignOrganizationSignIn(t, ctx, o.ti, "sweep-foreign-server")
		enableOrgAutoRefreshFeature(t, ctx, o.ti, foreign.organization, productfeatures.FeatureRemoteSessionAutoRefreshEnforced)
		crossed := seedRemoteMcpServerForLogin(t, ctx, o.ti.conn, o.projectID, foreign.signIn, "sweep-foreign-crossed", "https://crossed.example.com/mcp")
		offerRegistration(t, ctx, o.ti.conn, o.organization, o.projectID, crossed, o.registration)
		sessionID := o.grant(t, ctx, o.signIn, true, true)
		seedGramSession(t, ctx, o.ti, o.subject, foreign.signIn, "sweep-foreign-server", 24*time.Hour)
		window := newSweepWindow()
		window.keepaliveCutoff = time.Now().Add(time.Hour)
		q := repo.New(o.ti.conn)

		rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
		require.NoError(t, err)
		require.Empty(t, rows, "a session on the foreign sign-in does not put the grant under that organization's policy")
		_, err = q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(sessionID, foreign.organization))
		require.ErrorIs(t, err, pgx.ErrNoRows)

		seedGramSession(t, ctx, o.ti, o.subject, o.signIn, "sweep-foreign-server-home", 24*time.Hour)
		rows, err = q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
		require.NoError(t, err)
		require.Len(t, rows, 1, "a session on the sign-in the grant was consented through does")
		require.Equal(t, o.organization, rows[0].OrganizationID)
	})
}

type foreignOrganization struct {
	organization string
	signIn       uuid.UUID
}

func seedForeignOrganizationSignIn(t *testing.T, ctx context.Context, ti *testInstance, slug string) foreignOrganization {
	t.Helper()
	organization := createOrganization(t, ctx, ti.conn, slug+"-org")
	signIn, err := testrepo.New(ti.conn).InsertOrganizationTierUserSessionIssuerFixture(ctx, testrepo.InsertOrganizationTierUserSessionIssuerFixtureParams{
		OrganizationID:     conv.ToPGText(organization),
		Slug:               slug + "-org-usi",
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	return foreignOrganization{organization: organization, signIn: signIn}
}

// A grant with nothing to refresh on an offered registration is re-checked in
// the background through an endpoint of the offering server, and a claim the
// pass cannot use is given back.
func TestRecheckSweep_OfferedRegistrationWithoutASignInLink(t *testing.T) {
	t.Parallel()
	ctx, o := seedOfferedRegistration(t, "recheck-offered", "https://issuer.example.com/recheck-offered")
	enforceUpstreamAuth(t, ctx, o.ti.conn, o.organization)
	_, err := mcpendpointsrepo.New(o.ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: o.projectID, CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID: conv.ToNullUUID(o.serverID), MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Slug: "recheck-offered-endpoint",
	})
	require.NoError(t, err)
	sessionID := o.grant(t, ctx, o.signIn, false, false)
	seedGramSession(t, ctx, o.ti, o.subject, o.signIn, "recheck-offered", 24*time.Hour)
	window := newRecheckWindow()
	q := repo.New(o.ti.conn)

	rows, err := q.ClaimDueRemoteSessionRecheckCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, sessionID, rows[0].ID)
	require.Equal(t, []string{o.organization}, rows[0].OrganizationIds)

	candidate, err := q.GetDueRemoteSessionRecheckCandidate(ctx, window.candidateParams(sessionID, o.organization))
	require.NoError(t, err)
	require.Equal(t, sessionID, candidate.RemoteSession.ID)

	placements, err := q.GetRemoteSessionRecheckEndpoints(ctx, repo.GetRemoteSessionRecheckEndpointsParams{
		UserSessionIssuerID:   o.signIn,
		RemoteSessionClientID: o.registration,
		OrganizationIds:       []string{o.organization},
		SubjectUrn:            o.subject,
		NowTs:                 conv.ToPGTimestamptz(window.now),
	})
	require.NoError(t, err)
	require.Len(t, placements, 1)
	require.Equal(t, conv.ToNullUUID(o.serverID), placements[0].McpServerID)

	n, err := q.ClearRemoteSessionRecheckLease(ctx, repo.ClearRemoteSessionRecheckLeaseParams{ID: sessionID, OrganizationIds: []string{"another-organization"}})
	require.NoError(t, err)
	require.Zero(t, n, "a lease is given back only under the organizations it was claimed in")
	n, err = q.ClearRemoteSessionRecheckLease(ctx, repo.ClearRemoteSessionRecheckLeaseParams{ID: sessionID, OrganizationIds: rows[0].OrganizationIds})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	rows, err = q.ClaimDueRemoteSessionRecheckCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Len(t, rows, 1, "a lease given back is due again")
	o.requireNoLink(t, ctx)
}

func TestRecheckSweep_OfferedRegistrationOutOfReach(t *testing.T) {
	t.Parallel()
	for _, tc := range offeredReachCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, o := seedOfferedRegistration(t, "recheck-out", "https://issuer.example.com/recheck-out")
			if !tc.skipEnforcement {
				enforceUpstreamAuth(t, ctx, o.ti.conn, o.organization)
			}
			_, err := mcpendpointsrepo.New(o.ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
				ProjectID: o.projectID, CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				McpServerID: conv.ToNullUUID(o.serverID), MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Slug: "recheck-out-endpoint",
			})
			require.NoError(t, err)
			sessionID := o.grant(t, ctx, o.signIn, false, false)
			if !tc.skipSession {
				seedGramSession(t, ctx, o.ti, o.subject, o.signIn, "recheck-out", tc.sessionLifetime())
			}
			if tc.breakIt != nil {
				tc.breakIt(t, ctx, o)
			}
			window := newRecheckWindow()
			q := repo.New(o.ti.conn)

			rows, err := q.ClaimDueRemoteSessionRecheckCandidates(ctx, window.claimParams())
			require.NoError(t, err)
			require.Empty(t, rows)

			_, err = q.GetDueRemoteSessionRecheckCandidate(ctx, window.candidateParams(sessionID, o.organization))
			require.ErrorIs(t, err, pgx.ErrNoRows, "the claim and its re-read agree")

			placements, err := q.GetRemoteSessionRecheckEndpoints(ctx, repo.GetRemoteSessionRecheckEndpointsParams{
				UserSessionIssuerID:   o.signIn,
				RemoteSessionClientID: o.registration,
				OrganizationIds:       []string{o.organization},
				SubjectUrn:            o.subject,
				NowTs:                 conv.ToPGTimestamptz(window.now),
			})
			require.NoError(t, err)
			require.Empty(t, placements)
		})
	}
}

// The scheduled refresh activity runs for an offered registration: it reads the
// grant as due, refreshes it, and stamps the resource the offer decides.
func TestRemoteSessionRefreshActivity_RefreshesAnOfferedRegistration(t *testing.T) {
	t.Parallel()
	var posted atomic.Pointer[url.Values]
	upstream := newFormCapturingTokenUpstream(t, &posted)
	ctx, o := seedOfferedRegistration(t, "activity-offered", upstream.URL)
	enforceUpstreamAuth(t, ctx, o.ti.conn, o.organization)
	enableOrgAutoRefreshFeature(t, ctx, o.ti, o.organization, productfeatures.FeatureRemoteSessionAutoRefreshEnforced)
	subject := seedRefreshableSharedSession(t, ctx, o.ti, o.signIn, o.registration, "activity-offered")
	q := repo.New(o.ti.conn)
	session, err := q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: o.registration})
	require.NoError(t, err)
	require.NoError(t, q.SetRemoteSessionUpdatedAt(ctx, repo.SetRemoteSessionUpdatedAtParams{
		ID:        session.ID,
		ProjectID: conv.ToNullUUID(o.projectID),
		UpdatedAt: conv.ToPGTimestamptz(time.Now().Add(-25 * time.Hour)),
	}))
	seedGramSession(t, ctx, o.ti, subject, o.signIn, "activity-offered", 24*time.Hour)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	refresher := remotesessions.NewRefreshService(testenv.NewLogger(t), testenv.NewMeterProvider(t), o.ti.conn, testenv.NewEncryptionClient(t), policy, nil, cache.NoopCache)
	res, err := activities.NewRemoteSessionRefresh(testenv.NewLogger(t), o.ti.conn, refresher).Do(ctx, activities.RefreshRemoteSessionInput{SessionID: session.ID, OrganizationID: o.organization})
	require.NoError(t, err)
	require.False(t, res.RateLimited)

	form := posted.Load()
	require.NotNil(t, form, "the sweep must have sent the refresh grant")
	require.Equal(t, "refresh_token", form.Get("grant_type"))
	require.Equal(t, o.upstream, form.Get("resource"))

	stored, err := q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: o.registration})
	require.NoError(t, err)
	require.Equal(t, o.upstream, stored.Resource.String)
	o.requireNoLink(t, ctx)
}

func newFormCapturingTokenUpstream(t *testing.T, posted *atomic.Pointer[url.Values]) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		form := r.Form
		posted.Store(&form)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"offered-refreshed","token_type":"Bearer","expires_in":7200}`))
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

// A scheduled refresh of a grant stored with no resource names the resource its
// offering server decides, where typed auth clients are enforced, and refreshes
// as it did before elsewhere.
func TestRefreshNow_FallbackResourceFromTheOffer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		enforced bool
	}{
		{name: "enforced", enforced: true},
		{name: "not enforced", enforced: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var posted atomic.Pointer[url.Values]
			upstream := newFormCapturingTokenUpstream(t, &posted)

			ctx, o := seedOfferedRegistration(t, "fallback-offered", upstream.URL)
			if tc.enforced {
				enforceUpstreamAuth(t, ctx, o.ti.conn, o.organization)
			}
			subject := seedRefreshableSharedSession(t, ctx, o.ti, o.signIn, o.registration, "fallback-offered")
			session, err := repo.New(o.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: o.registration})
			require.NoError(t, err)
			require.False(t, session.Resource.Valid)

			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
			require.NoError(t, err)
			refresher := remotesessions.NewRefreshService(testenv.NewLogger(t), testenv.NewMeterProvider(t), o.ti.conn, testenv.NewEncryptionClient(t), policy, nil, cache.NoopCache)
			result, err := refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerScheduled, remotesessions.RefreshAttribution{})
			require.NoError(t, err)
			require.Equal(t, remotesessionmetrics.RefreshOutcomeRefreshed, result.Outcome)

			form := posted.Load()
			require.NotNil(t, form)
			if tc.enforced {
				require.Equal(t, o.upstream, form.Get("resource"))
				return
			}
			require.Empty(t, form.Get("resource"))
		})
	}
}
