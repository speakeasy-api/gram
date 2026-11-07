package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func typedLifecycleGrant(t *testing.T, ctx context.Context, f authClientTokenFixture, subject urn.SessionSubject, resource string) repo.RemoteSession {
	t.Helper()
	encrypted, err := f.enc.Encrypt([]byte("typed-token-" + resource))
	require.NoError(t, err)
	row, err := repo.New(f.ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn: subject, RemoteSessionClientID: f.clientID, UserSessionIssuerID: f.signInA,
		Resource: conv.ToPGTextEmpty(resource), AccessTokenEncrypted: encrypted,
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{},
	})
	require.NoError(t, err)
	return row
}

func TestAuthClientResourceLifecycle_NoLinkBothSchemas(t *testing.T) {
	t.Parallel()
	for _, expanded := range []bool{false, true} {
		name := "old_indexes"
		if expanded {
			name = "resource_indexes"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newAuthClientTokenFixture(t, "typed-lifecycle")
			if expanded {
				expandResourceGrantIndexes(t, ctx, f.ti.conn)
			} else {
				require.NoError(t, testrepo.New(f.ti.conn).CreateLegacyRemoteSessionSubjectClientIndexFixture(ctx))
			}
			q := repo.New(f.ti.conn)
			require.NoError(t, q.DeleteUserSessionIssuerAttachmentsForRemoteSessionClient(ctx, repo.DeleteUserSessionIssuerAttachmentsForRemoteSessionClientParams{RemoteSessionClientID: f.clientID, ProjectID: conv.ToNullUUID(f.projectID)}))
			subject := urn.NewUserSubject("typed-lifecycle-owner")
			const resourceA = "https://typed-a.example/mcp"
			a := typedLifecycleGrant(t, ctx, f, subject, resourceA)
			var siblings []repo.RemoteSession
			if expanded {
				siblings = append(siblings, typedLifecycleGrant(t, ctx, f, subject, "https://typed-b.example/mcp"), typedLifecycleGrant(t, ctx, f, subject, ""))
			}
			unchanged := func(row repo.RemoteSession) {
				t.Helper()
				current, err := q.GetActiveRemoteSessionByID(ctx, repo.GetActiveRemoteSessionByIDParams{ID: row.ID, SubjectUrn: subject, RemoteSessionClientID: f.clientID})
				require.NoError(t, err)
				require.Equal(t, row, current)
			}
			// Compatibility APIs retain their issuer-link boundary.
			n, err := f.mgr.DisconnectRemoteSessionForResource(ctx, subject, f.projectID, f.organizationID, f.signInA, f.clientID, resourceA)
			require.NoError(t, err)
			require.Zero(t, n)
			_, err = f.mgr.RefreshRemoteSessionForResource(ctx, remotesessions.RefreshAttribution{}, subject, f.projectID, f.organizationID, f.signInA, f.clientID, resourceA)
			require.ErrorIs(t, err, remotesessions.ErrRemoteSessionNotRefreshable)
			unchanged(a)
			n, err = f.mgr.DisconnectAuthClientRemoteSessionForResource(ctx, subject, f.projectID, f.organizationID, f.clientID, "")
			require.Error(t, err)
			require.Zero(t, n)
			n, err = f.mgr.SetAuthClientRemoteSessionAutoRefreshForResource(ctx, subject, f.projectID, f.organizationID, f.clientID, "", true)
			require.Error(t, err)
			require.Zero(t, n)
			_, err = f.mgr.RefreshAuthClientRemoteSessionForResource(ctx, remotesessions.RefreshAttribution{}, subject, f.projectID, f.organizationID, f.clientID, "")
			require.Error(t, err)
			// Neither another caller nor a foreign tenant may change this user's source.
			for _, denied := range []struct {
				subject urn.SessionSubject
				project uuid.UUID
				org     string
			}{
				{urn.NewUserSubject("typed-stranger"), f.projectID, f.organizationID},
				{subject, uuid.New(), f.organizationID}, {subject, f.projectID, "foreign-organization"},
			} {
				n, err := f.mgr.DisconnectAuthClientRemoteSessionForResource(ctx, denied.subject, denied.project, denied.org, f.clientID, resourceA)
				require.NoError(t, err)
				require.Zero(t, n)
				n, err = f.mgr.SetAuthClientRemoteSessionAutoRefreshForResource(ctx, denied.subject, denied.project, denied.org, f.clientID, resourceA, true)
				require.NoError(t, err)
				require.Zero(t, n)
				_, err = f.mgr.RefreshAuthClientRemoteSessionForResource(ctx, remotesessions.RefreshAttribution{}, denied.subject, denied.project, denied.org, f.clientID, resourceA)
				require.ErrorIs(t, err, remotesessions.ErrRemoteSessionNotRefreshable)
			}
			unchanged(a)
			if expanded {
				n, err = f.mgr.DisconnectAuthClientRemoteSession(ctx, subject, f.projectID, f.organizationID, f.clientID)
				require.Error(t, err)
				require.Zero(t, n)
				n, err = f.mgr.SetAuthClientRemoteSessionAutoRefresh(ctx, subject, f.projectID, f.organizationID, f.clientID, true)
				require.Error(t, err)
				require.Zero(t, n)
				_, err = f.mgr.RefreshAuthClientRemoteSession(ctx, remotesessions.RefreshAttribution{}, subject, f.projectID, f.organizationID, f.clientID)
				require.Error(t, err)
				// A missing focused audience must not infer the legacy NULL sibling.
				n, err = f.mgr.DisconnectAuthClientRemoteSessionForResource(ctx, subject, f.projectID, f.organizationID, f.clientID, "https://typed-c.example/mcp")
				require.Error(t, err)
				require.Zero(t, n)
				n, err = f.mgr.SetAuthClientRemoteSessionAutoRefreshForResource(ctx, subject, f.projectID, f.organizationID, f.clientID, "https://typed-c.example/mcp", true)
				require.Error(t, err)
				require.Zero(t, n)
				_, err = f.mgr.RefreshAuthClientRemoteSessionForResource(ctx, remotesessions.RefreshAttribution{}, subject, f.projectID, f.organizationID, f.clientID, "https://typed-c.example/mcp")
				require.Error(t, err)
				unchanged(a)
			} else {
				n, err = f.mgr.SetAuthClientRemoteSessionAutoRefresh(ctx, subject, f.projectID, f.organizationID, f.clientID, false)
				require.NoError(t, err)
				require.EqualValues(t, 1, n)
			}
			n, err = f.mgr.SetAuthClientRemoteSessionAutoRefreshForResource(ctx, subject, f.projectID, f.organizationID, f.clientID, resourceA+"/", true)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
			current, err := q.GetActiveRemoteSessionByID(ctx, repo.GetActiveRemoteSessionByIDParams{ID: a.ID, SubjectUrn: subject, RemoteSessionClientID: f.clientID})
			require.NoError(t, err)
			require.True(t, current.AutoRefresh)
			require.Equal(t, a.UpdatedAt, current.UpdatedAt, "preferences never change the refresh CAS timestamp")
			for _, sibling := range siblings {
				unchanged(sibling)
			}
			n, err = f.mgr.DisconnectAuthClientRemoteSessionForResource(ctx, subject, f.projectID, f.organizationID, f.clientID, resourceA)
			require.NoError(t, err)
			require.EqualValues(t, 1, n)
			for _, sibling := range siblings {
				unchanged(sibling)
			}
		})
	}
}

func TestAuthClientResourceLifecycle_CASRejectsReconsent(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "typed-cas")
	expandResourceGrantIndexes(t, ctx, f.ti.conn)
	subject := urn.NewUserSubject("typed-cas-owner")
	a := typedLifecycleGrant(t, ctx, f, subject, "https://typed-cas-a.example/mcp")
	b := typedLifecycleGrant(t, ctx, f, subject, "https://typed-cas-b.example/mcp")
	replacement := typedLifecycleGrant(t, ctx, f, subject, a.Resource.String)
	require.Greater(t, replacement.GrantGeneration, a.GrantGeneration)
	q := repo.New(f.ti.conn)
	n, err := q.SetRemoteSessionAutoRefreshInTenant(ctx, repo.SetRemoteSessionAutoRefreshInTenantParams{SubjectUrn: subject, RemoteSessionClientID: f.clientID, ProjectID: f.projectID, OrganizationID: f.organizationID, ID: a.ID, ExpectedUpdatedAt: a.UpdatedAt, Resource: a.Resource.String, AutoRefresh: true})
	require.NoError(t, err)
	require.Zero(t, n)
	removed, err := q.SoftDeleteRemoteSessionBySubjectAndClientInTenant(ctx, repo.SoftDeleteRemoteSessionBySubjectAndClientInTenantParams{SubjectUrn: subject, RemoteSessionClientID: f.clientID, ProjectID: f.projectID, OrganizationID: f.organizationID, ID: a.ID, ExpectedUpdatedAt: a.UpdatedAt, Resource: a.Resource.String})
	require.NoError(t, err)
	require.Empty(t, removed)
	// Even a fresh ID/timestamp cannot be used for a sibling audience.
	removed, err = q.SoftDeleteRemoteSessionBySubjectAndClientInTenant(ctx, repo.SoftDeleteRemoteSessionBySubjectAndClientInTenantParams{SubjectUrn: subject, RemoteSessionClientID: f.clientID, ProjectID: f.projectID, OrganizationID: f.organizationID, ID: replacement.ID, ExpectedUpdatedAt: replacement.UpdatedAt, Resource: b.Resource.String})
	require.NoError(t, err)
	require.Empty(t, removed)
	// Mutation-time provider admission is rechecked, even with a fresh source CAS.
	_, err = testrepo.New(f.ti.conn).SoftDeleteAttachmentIssuerFixture(ctx, testrepo.SoftDeleteAttachmentIssuerFixtureParams{ID: f.issuerID, ProjectID: conv.ToNullUUID(f.projectID)})
	require.NoError(t, err)
	n, err = q.SetRemoteSessionAutoRefreshInTenant(ctx, repo.SetRemoteSessionAutoRefreshInTenantParams{SubjectUrn: subject, RemoteSessionClientID: f.clientID, ProjectID: f.projectID, OrganizationID: f.organizationID, ID: replacement.ID, ExpectedUpdatedAt: replacement.UpdatedAt, Resource: replacement.Resource.String, AutoRefresh: true})
	require.NoError(t, err)
	require.Zero(t, n)
	removed, err = q.SoftDeleteRemoteSessionBySubjectAndClientInTenant(ctx, repo.SoftDeleteRemoteSessionBySubjectAndClientInTenantParams{SubjectUrn: subject, RemoteSessionClientID: f.clientID, ProjectID: f.projectID, OrganizationID: f.organizationID, ID: replacement.ID, ExpectedUpdatedAt: replacement.UpdatedAt, Resource: replacement.Resource.String})
	require.NoError(t, err)
	require.Empty(t, removed)
	for _, row := range []repo.RemoteSession{replacement, b} {
		current, err := q.GetActiveRemoteSessionByID(ctx, repo.GetActiveRemoteSessionByIDParams{ID: row.ID, SubjectUrn: subject, RemoteSessionClientID: f.clientID})
		require.NoError(t, err)
		require.Equal(t, row, current)
	}
}

func TestAuthClientResourceLifecycle_RefreshRotatesOnlySelectedNoLinkGrant(t *testing.T) {
	t.Parallel()
	var spy upstreamSpy
	const resourceA = "https://typed-refresh-a.example/mcp"
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "typed-no-link-refresh", conv.ToPGText(resourceA), spyRefreshHandler(&spy))
	expandResourceGrantIndexes(t, ctx, ti.conn)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := repo.New(ti.conn)
	a, err := q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: clientID, Resource: resourceA})
	require.NoError(t, err)
	// The shared refresh helper sets a client audience but seeds an unbound grant.
	// Focused mutations require an explicit stored resource, never NULL inference.
	require.NoError(t, testrepo.New(ti.conn).SetRemoteSessionResourceByIDFixture(ctx, testrepo.SetRemoteSessionResourceByIDFixtureParams{ID: a.ID, Resource: conv.ToPGText(resourceA)}))
	a, err = q.GetActiveRemoteSessionByID(ctx, repo.GetActiveRemoteSessionByIDParams{ID: a.ID, SubjectUrn: subject, RemoteSessionClientID: clientID})
	require.NoError(t, err)
	require.NoError(t, q.DeleteUserSessionIssuerAttachmentsForRemoteSessionClient(ctx, repo.DeleteUserSessionIssuerAttachmentsForRemoteSessionClientParams{RemoteSessionClientID: clientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)}))
	enc := testenv.NewEncryptionClient(t)
	token, err := enc.Encrypt([]byte("typed-refresh-b"))
	require.NoError(t, err)
	b, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{SubjectUrn: subject, UserSessionIssuerID: a.UserSessionIssuerID, RemoteSessionClientID: clientID, Resource: conv.ToPGText("https://typed-refresh-b.example/mcp"), AccessTokenEncrypted: token, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{}})
	require.NoError(t, err)
	_, err = mgr.RefreshAuthClientRemoteSession(ctx, remotesessions.RefreshAttribution{}, subject, *auth.ProjectID, auth.ActiveOrganizationID, clientID)
	require.Error(t, err)
	result, err := mgr.RefreshAuthClientRemoteSessionForResource(ctx, remotesessions.RefreshAttribution{}, subject, *auth.ProjectID, auth.ActiveOrganizationID, clientID, resourceA)
	require.NoError(t, err)
	require.Equal(t, "refreshed-access", result.AccessToken)
	require.Equal(t, a.ID, result.Session.ID)
	require.Equal(t, a.GrantGeneration, result.Session.GrantGeneration)
	require.Equal(t, resourceA, spy.form.Get("resource"))
	currentB, err := q.GetActiveRemoteSessionByID(ctx, repo.GetActiveRemoteSessionByIDParams{ID: b.ID, SubjectUrn: subject, RemoteSessionClientID: clientID})
	require.NoError(t, err)
	require.Equal(t, b, currentB)
	require.NotEqual(t, a.RefreshTokenEncrypted, result.Session.RefreshTokenEncrypted, "refresh rotation is persisted only on A")
}

func TestAuthClientResourceLifecycle_DisconnectRevokesOnlySelectedNoLinkGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	upstream := newRevocationUpstream(t, spy)
	fx := seedRevocableSession(t, ctx, ti, "typed-resource-revoke", upstream.URL+"/revoke", "fixture-secret", true)
	expandResourceGrantIndexes(t, ctx, ti.conn)
	q := repo.New(ti.conn)
	a, err := q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: fx.subject, RemoteSessionClientID: fx.clientID})
	require.NoError(t, err)
	const resourceA = "https://typed-revoke-a.example/mcp"
	require.NoError(t, testrepo.New(ti.conn).SetRemoteSessionResourceByIDFixture(ctx, testrepo.SetRemoteSessionResourceByIDFixtureParams{ID: a.ID, Resource: conv.ToPGText(resourceA)}))
	require.NoError(t, q.DeleteUserSessionIssuerAttachmentsForRemoteSessionClient(ctx, repo.DeleteUserSessionIssuerAttachmentsForRemoteSessionClientParams{RemoteSessionClientID: fx.clientID, ProjectID: conv.ToNullUUID(fx.projectID)}))
	enc := testenv.NewEncryptionClient(t)
	bAccess, err := enc.Encrypt([]byte("independent-b-access"))
	require.NoError(t, err)
	bRefresh, err := enc.Encrypt([]byte("independent-b-refresh"))
	require.NoError(t, err)
	b, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{SubjectUrn: fx.subject, UserSessionIssuerID: a.UserSessionIssuerID, RemoteSessionClientID: fx.clientID, Resource: conv.ToPGText("https://typed-revoke-b.example/mcp"), AccessTokenEncrypted: bAccess, RefreshTokenEncrypted: conv.ToPGText(bRefresh), Scopes: []string{}})
	require.NoError(t, err)
	n, err := newDisconnectChallengeManager(t, ti).DisconnectAuthClientRemoteSessionForResource(ctx, fx.subject, fx.projectID, fx.organizationID, fx.clientID, resourceA)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	calls, form, _ := spy.snapshot()
	require.Equal(t, 1, calls)
	require.Equal(t, fx.refreshToken, form.Get("token"))
	currentB, err := q.GetActiveRemoteSessionByID(ctx, repo.GetActiveRemoteSessionByIDParams{ID: b.ID, SubjectUrn: fx.subject, RemoteSessionClientID: fx.clientID})
	require.NoError(t, err)
	require.Equal(t, b, currentB)
}

func TestAuthClientResourceLifecycle_RejectsForeignConsentOnGlobalRegistration(t *testing.T) {
	t.Parallel()
	ctx, f := newReconsentFixture(t, "typed-foreign-consent")
	row := f.store(t, ctx, f.otherOrg, f.otherSignIn, "foreign-consent-token")
	const resource = "https://typed-foreign.example/mcp"
	tq := testrepo.New(f.ti.conn)
	require.NoError(t, tq.SetRemoteSessionResourceByIDFixture(ctx, testrepo.SetRemoteSessionResourceByIDFixtureParams{ID: row.ID, Resource: conv.ToPGText(resource)}))
	for _, signIn := range []uuid.UUID{f.signInA, f.otherSignIn} {
		_, err := tq.UnlinkAttachmentClientFixture(ctx, testrepo.UnlinkAttachmentClientFixtureParams{RemoteSessionClientID: f.clientID, UserSessionIssuerID: signIn})
		require.NoError(t, err)
	}
	n, err := f.mgr.DisconnectAuthClientRemoteSessionForResource(ctx, f.member, f.projectID, f.organizationID, f.clientID, resource)
	require.NoError(t, err)
	require.Zero(t, n)
	n, err = f.mgr.SetAuthClientRemoteSessionAutoRefreshForResource(ctx, f.member, f.projectID, f.organizationID, f.clientID, resource, true)
	require.NoError(t, err)
	require.Zero(t, n)
	_, err = f.mgr.RefreshAuthClientRemoteSessionForResource(ctx, remotesessions.RefreshAttribution{}, f.member, f.projectID, f.organizationID, f.clientID, resource)
	require.ErrorIs(t, err, remotesessions.ErrRemoteSessionNotRefreshable)
	n, err = f.mgr.SetAuthClientRemoteSessionAutoRefreshForResource(ctx, f.member, f.otherProject, f.otherOrg, f.clientID, resource, true)
	require.NoError(t, err)
	require.EqualValues(t, 1, n, "the actual consenting tenant remains admitted without a link")
}

func TestAuthClientResourceLifecycle_LegacySingletonWithoutLink(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "typed-legacy-singleton")
	q := repo.New(f.ti.conn)
	require.NoError(t, q.DeleteUserSessionIssuerAttachmentsForRemoteSessionClient(ctx, repo.DeleteUserSessionIssuerAttachmentsForRemoteSessionClientParams{RemoteSessionClientID: f.clientID, ProjectID: conv.ToNullUUID(f.projectID)}))
	subject := urn.NewUserSubject("typed-legacy-singleton-owner")
	legacy := typedLifecycleGrant(t, ctx, f, subject, "")
	n, err := f.mgr.SetAuthClientRemoteSessionAutoRefreshForResource(ctx, subject, f.projectID, f.organizationID, f.clientID, "https://not-inferred.example/mcp", true)
	require.Error(t, err)
	require.Zero(t, n)
	n, err = f.mgr.SetAuthClientRemoteSessionAutoRefresh(ctx, subject, f.projectID, f.organizationID, f.clientID, true)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	current, err := q.GetActiveRemoteSessionByID(ctx, repo.GetActiveRemoteSessionByIDParams{ID: legacy.ID, SubjectUrn: subject, RemoteSessionClientID: f.clientID})
	require.NoError(t, err)
	require.True(t, current.AutoRefresh)
	require.False(t, current.Resource.Valid)
	require.Equal(t, legacy.UpdatedAt, current.UpdatedAt)
	n, err = f.mgr.DisconnectAuthClientRemoteSession(ctx, subject, f.projectID, f.organizationID, f.clientID)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

func TestAuthClientResourceLifecycle_RefreshLegacySingletonWithoutLink(t *testing.T) {
	t.Parallel()
	var spy upstreamSpy
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "typed-legacy-refresh", conv.ToPGTextEmpty(""), spyRefreshHandler(&spy))
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := repo.New(ti.conn)
	require.NoError(t, q.DeleteUserSessionIssuerAttachmentsForRemoteSessionClient(ctx, repo.DeleteUserSessionIssuerAttachmentsForRemoteSessionClientParams{RemoteSessionClientID: clientID, ProjectID: conv.ToNullUUID(*auth.ProjectID)}))
	result, err := mgr.RefreshAuthClientRemoteSession(ctx, remotesessions.RefreshAttribution{}, subject, *auth.ProjectID, auth.ActiveOrganizationID, clientID)
	require.NoError(t, err)
	require.Equal(t, "refreshed-access", result.AccessToken)
	require.False(t, result.Session.Resource.Valid)
	require.Empty(t, spy.form.Get("resource"))
}
