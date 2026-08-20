// sharedcredential_test.go covers the shared-credential lifecycle (AGE-3285):
// a remote_session is one upstream grant per (subject_urn,
// remote_session_client_id) and its user_session_issuer_id is provenance
// only. Every bound user_session_issuer must see and control the same
// credential — status, disconnect, auto-refresh, explicit refresh, revoke
// cascade, and keepalive eligibility — while an unbound issuer must be
// refused (fail closed) by the tenant-scoped client binding.
//
// The fixtures mint the credential through issuer A ("provenance") and then
// bind the same client to a sibling issuer B; the assertions drive each
// lifecycle surface through B.

package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// bindSiblingIssuer creates a second user_session_issuer in the fixture's
// project and binds the fixture's client to it, returning the sibling's id.
func bindSiblingIssuer(t *testing.T, ctx context.Context, ti *testInstance, clientID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	sibling := createUserSessionIssuer(t, ctx, ti.conn, slug)
	err := repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   sibling,
	})
	require.NoError(t, err)
	return sibling
}

func TestRemoteSessionStatuses_SiblingIssuerSeesSharedCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fx := seedRevocableSession(t, ctx, ti, "shared-status", "", "s3cret", true)
	sibling := bindSiblingIssuer(t, ctx, ti, fx.clientID, "usi-shared-status-sibling")

	statuses, err := newDisconnectChallengeManager(t, ti).RemoteSessionStatuses(ctx, fx.subject, fx.projectID, fx.organizationID, sibling)
	require.NoError(t, err)
	state, ok := statuses[fx.clientID]
	require.True(t, ok, "a credential minted through the provenance issuer must report status under a sibling issuer bound to the same client")
	require.Equal(t, remotesessions.RemoteSessionActive, state.Status)
}

func TestRemoteSessionStatuses_UnboundIssuerSeesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fx := seedRevocableSession(t, ctx, ti, "shared-status-unbound", "", "s3cret", true)
	unbound := createUserSessionIssuer(t, ctx, ti.conn, "usi-x-shared-status-unbound")

	statuses, err := newDisconnectChallengeManager(t, ti).RemoteSessionStatuses(ctx, fx.subject, fx.projectID, fx.organizationID, unbound)
	require.NoError(t, err)
	require.NotContains(t, statuses, fx.clientID, "an issuer with no client binding must not see the credential")
}

func TestDisconnectRemoteSession_SiblingIssuerDestroysSharedCredentialGlobally(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	upstream := newRevocationUpstream(t, spy)
	fx := seedRevocableSession(t, ctx, ti, "shared-disconnect", upstream.URL, "s3cret", true)
	sibling := bindSiblingIssuer(t, ctx, ti, fx.clientID, "usi-shared-disconnect-sibling")

	n, err := newDisconnectChallengeManager(t, ti).DisconnectRemoteSession(ctx, fx.subject, fx.projectID, fx.organizationID, sibling, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "a disconnect from any bound surface must destroy the shared credential")

	requireSessionRevoked(t, ctx, ti, fx)

	calls, form, _ := spy.snapshot()
	require.Equal(t, 1, calls, "the disconnect must attempt upstream revocation")
	require.Equal(t, fx.refreshToken, form.Get("token"))
}

func TestDisconnectRemoteSession_UnboundIssuerFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	spy := &revocationSpy{}
	upstream := newRevocationUpstream(t, spy)
	fx := seedRevocableSession(t, ctx, ti, "shared-disconnect-unbound", upstream.URL, "s3cret", true)
	unbound := createUserSessionIssuer(t, ctx, ti.conn, "usi-x-shared-disconnect-unbound")

	n, err := newDisconnectChallengeManager(t, ti).DisconnectRemoteSession(ctx, fx.subject, fx.projectID, fx.organizationID, unbound, fx.clientID)
	require.NoError(t, err)
	require.Zero(t, n, "an unbound issuer must not be able to disconnect the credential")

	_, err = repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.NoError(t, err, "the credential must survive an unbound issuer's disconnect attempt")

	calls, _, _ := spy.snapshot()
	require.Zero(t, calls, "nothing was disconnected, so nothing may be revoked upstream")
}

func TestSetRemoteSessionAutoRefresh_SiblingIssuerUpdatesSharedCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fx := seedRevocableSession(t, ctx, ti, "shared-autorefresh", "", "s3cret", true)
	sibling := bindSiblingIssuer(t, ctx, ti, fx.clientID, "usi-shared-autorefresh-sibling")

	n, err := newDisconnectChallengeManager(t, ti).SetRemoteSessionAutoRefresh(ctx, fx.subject, fx.projectID, fx.organizationID, sibling, fx.clientID, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "a preference set from any bound surface must land on the shared credential")

	session, err := repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.NoError(t, err)
	require.True(t, session.AutoRefresh)
}

func TestSetRemoteSessionAutoRefresh_UnboundIssuerNoOp(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fx := seedRevocableSession(t, ctx, ti, "shared-autorefresh-unbound", "", "s3cret", true)
	unbound := createUserSessionIssuer(t, ctx, ti.conn, "usi-x-shared-autorefresh-unbound")

	n, err := newDisconnectChallengeManager(t, ti).SetRemoteSessionAutoRefresh(ctx, fx.subject, fx.projectID, fx.organizationID, unbound, fx.clientID, true)
	require.NoError(t, err)
	require.Zero(t, n, "an unbound issuer must not be able to flip the credential's preference")
}

// newRefreshTokenUpstream stands up a fake authorization server whose token
// endpoint answers a refresh_token grant with accessToken, so a lifecycle test
// can drive a real RefreshNow round trip instead of stopping at the
// authorization probe.
func newRefreshTokenUpstream(t *testing.T, accessToken string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			http.Error(w, "unexpected grant_type", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + accessToken + `","token_type":"Bearer","expires_in":7200}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// seedRefreshableSharedSession upserts a remote_session on clientID whose
// refresh token decrypts under the fixed test key, returning the subject. The
// provenance issuer is usi; sibling-issuer tests bind additional issuers to
// the same client afterwards.
func seedRefreshableSharedSession(t *testing.T, ctx context.Context, ti *testInstance, usi uuid.UUID, clientID uuid.UUID, slug string) urn.SessionSubject {
	t.Helper()

	enc := testenv.NewEncryptionClient(t)
	accessEnc, err := enc.Encrypt([]byte(slug + "-access"))
	require.NoError(t, err)
	refreshEnc, err := enc.Encrypt([]byte(slug + "-refresh"))
	require.NoError(t, err)

	subject := urn.NewUserSubject("subject-" + slug)
	_, err = repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   usi,
		RemoteSessionClientID: clientID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: conv.ToPGText(refreshEnc),
		Scopes:                []string{},
		AutoRefresh:           false,
	})
	require.NoError(t, err)
	return subject
}

func TestRefreshRemoteSession_SiblingIssuerRefreshesSharedCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	upstream := newRefreshTokenUpstream(t, "sibling-refreshed-access")
	issuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn,
		conv.ToNullUUID(*authCtx.ProjectID), conv.ToPGText(authCtx.ActiveOrganizationID),
		"shared-refresh-sibling-issuer", upstream.URL)
	provenance := createUserSessionIssuer(t, ctx, ti.conn, "usi-shared-refresh-sibling-a")
	clientID := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, *authCtx.ProjectID, issuerID, "shared-refresh-sibling-cid")
	require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   provenance,
	}))
	subject := seedRefreshableSharedSession(t, ctx, ti, provenance, clientID, "shared-refresh-sibling")
	sibling := bindSiblingIssuer(t, ctx, ti, clientID, "usi-shared-refresh-sibling-b")

	result, err := newDisconnectChallengeManager(t, ti).RefreshRemoteSession(ctx, subject, *authCtx.ProjectID, authCtx.ActiveOrganizationID, sibling, clientID, "")
	require.NoError(t, err, "a sibling issuer bound to the same client must be able to refresh the shared credential")
	require.Equal(t, remotesessions.RefreshOutcomeRefreshed, result.Outcome)
	require.Equal(t, "sibling-refreshed-access", result.AccessToken)

	stored, err := repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
	})
	require.NoError(t, err)
	plain, err := testenv.NewEncryptionClient(t).Decrypt(stored.AccessTokenEncrypted)
	require.NoError(t, err)
	require.Equal(t, "sibling-refreshed-access", plain, "the rotated token must be persisted on the shared row")
}

func TestRefreshRemoteSession_OrgLevelClientBindingAuthorizes(t *testing.T) {
	t.Parallel()

	// An organization-level client (project_id NULL, organization_id set)
	// bound to the project's user_session_issuer must pass the binding probe
	// through its org arm (c.organization_id = @org) and refresh end to end.
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	upstream := newRefreshTokenUpstream(t, "org-refreshed-access")
	issuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn,
		uuid.NullUUID{}, conv.ToPGText(authCtx.ActiveOrganizationID),
		"shared-refresh-org-issuer", upstream.URL)
	usi := createUserSessionIssuer(t, ctx, ti.conn, "usi-shared-refresh-org")
	clientID := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, issuerID, "shared-refresh-org-cid", usi)
	subject := seedRefreshableSharedSession(t, ctx, ti, usi, clientID, "shared-refresh-org")

	mgr := newDisconnectChallengeManager(t, ti)

	result, err := mgr.RefreshRemoteSession(ctx, subject, *authCtx.ProjectID, authCtx.ActiveOrganizationID, usi, clientID, "")
	require.NoError(t, err, "the org-level client arm of the binding probe must authorize the refresh")
	require.Equal(t, remotesessions.RefreshOutcomeRefreshed, result.Outcome)
	require.Equal(t, "org-refreshed-access", result.AccessToken)

	// The re-scoped lifecycle queries carry the same org arm: a write through
	// the org-level client must land.
	n, err := mgr.SetRemoteSessionAutoRefresh(ctx, subject, *authCtx.ProjectID, authCtx.ActiveOrganizationID, usi, clientID, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "the lifecycle queries' org-level client arm must match the shared credential")
}

func TestSharedCredentialLifecycle_WrongProjectRejected(t *testing.T) {
	t.Parallel()

	// The issuer IS bound to the client, but the caller presents a foreign
	// project id: the usi.project_id predicate must reject every lifecycle
	// surface even though the binding row exists.
	ctx, ti := newTestService(t)
	fx := seedRevocableSession(t, ctx, ti, "shared-wrong-project", "", "s3cret", true)
	foreignProject := createProject(t, ctx, ti.conn, "shared-wrong-project-foreign")

	mgr := newDisconnectChallengeManager(t, ti)

	statuses, err := mgr.RemoteSessionStatuses(ctx, fx.subject, foreignProject, fx.organizationID, fx.userIssuerID)
	require.NoError(t, err)
	require.NotContains(t, statuses, fx.clientID, "a foreign project must not see the credential's status")

	n, err := mgr.SetRemoteSessionAutoRefresh(ctx, fx.subject, foreignProject, fx.organizationID, fx.userIssuerID, fx.clientID, true)
	require.NoError(t, err)
	require.Zero(t, n, "a foreign project must not flip the credential's preference")

	n, err = mgr.DisconnectRemoteSession(ctx, fx.subject, foreignProject, fx.organizationID, fx.userIssuerID, fx.clientID)
	require.NoError(t, err)
	require.Zero(t, n, "a foreign project must not disconnect the credential")

	_, err = mgr.RefreshRemoteSession(ctx, fx.subject, foreignProject, fx.organizationID, fx.userIssuerID, fx.clientID, "")
	require.ErrorIs(t, err, remotesessions.ErrRemoteSessionNotRefreshable, "a foreign project must not spend the refresh grant")

	_, err = repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.NoError(t, err, "the credential must survive the foreign project's lifecycle attempts")
}

func TestRefreshRemoteSession_UnboundIssuerFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	fx := seedRevocableSession(t, ctx, ti, "shared-refresh-unbound", "", "s3cret", true)
	unbound := createUserSessionIssuer(t, ctx, ti.conn, "usi-x-shared-refresh-unbound")

	_, err := newDisconnectChallengeManager(t, ti).RefreshRemoteSession(ctx, fx.subject, fx.projectID, authCtx.ActiveOrganizationID, unbound, fx.clientID, "")
	require.ErrorIs(t, err, remotesessions.ErrRemoteSessionNotRefreshable, "an unbound issuer must not be able to spend the shared refresh grant")
}

func TestSoftDeleteSubjectSessions_SiblingIssuerRevokeDestroysSharedCredential(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fx := seedRevocableSession(t, ctx, ti, "shared-revoke-cascade", "", "s3cret", true)
	sibling := bindSiblingIssuer(t, ctx, ti, fx.clientID, "usi-shared-revoke-sibling")

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	revoker := remotesessions.NewUpstreamRevoker(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		testenv.NewEncryptionClient(t),
		policy,
	)

	creds, err := revoker.SoftDeleteSubjectSessions(ctx, ti.conn, fx.subject, sibling, fx.projectID, fx.organizationID)
	require.NoError(t, err)
	require.Len(t, creds, 1, "revoking the subject's sessions under a sibling issuer must tombstone the shared credential")
	require.Equal(t, fx.clientID, creds[0].RemoteSessionClientID)

	requireSessionRevoked(t, ctx, ti, fx)
}

func TestSoftDeleteSubjectSessions_UnboundIssuerRevokesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fx := seedRevocableSession(t, ctx, ti, "shared-revoke-unbound", "", "s3cret", true)
	unbound := createUserSessionIssuer(t, ctx, ti.conn, "usi-x-shared-revoke-unbound")

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	revoker := remotesessions.NewUpstreamRevoker(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		testenv.NewEncryptionClient(t),
		policy,
	)

	creds, err := revoker.SoftDeleteSubjectSessions(ctx, ti.conn, fx.subject, unbound, fx.projectID, fx.organizationID)
	require.NoError(t, err)
	require.Empty(t, creds)

	_, err = repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.NoError(t, err, "an unbound issuer's revoke must not touch the credential")
}

func TestRefreshSweep_SiblingIssuerKeepsCredentialEligible(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Provenance issuer A mints the credential but its subject holds no live
	// Gram session under A; sibling issuer B is bound to the same client and
	// does. The keepalive must consider the credential eligible through B.
	issuerID := createRemoteIssuer(t, ctx, ti, "shared-sweep-issuer", "")
	provenance := createUserSessionIssuer(t, ctx, ti.conn, "shared-sweep-usi-a")
	clientID := createRemoteClient(t, ctx, ti, issuerID, provenance.String(), "shared-sweep-client")
	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)

	subject := urn.NewUserSubject("subject-shared-sweep")
	session, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   provenance,
		RemoteSessionClientID: clientUUID,
		AccessTokenEncrypted:  "access-ciphertext",
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: conv.ToPGText("refresh-ciphertext"),
		Scopes:                []string{},
		AutoRefresh:           true,
	})
	require.NoError(t, err)
	require.NoError(t, repo.New(ti.conn).SetRemoteSessionUpdatedAt(ctx, repo.SetRemoteSessionUpdatedAtParams{
		ID:        session.ID,
		ProjectID: conv.ToNullUUID(*authCtx.ProjectID),
		UpdatedAt: conv.ToPGTimestamptz(time.Now().Add(-25 * time.Hour)),
	}))

	sibling := bindSiblingIssuer(t, ctx, ti, clientUUID, "shared-sweep-usi-b")
	seedGramSession(t, ctx, ti, subject, sibling, "shared-sweep-b", 24*time.Hour)
	enableOrgAutoRefreshFeature(t, ctx, ti, authCtx.ActiveOrganizationID, productfeatures.FeatureRemoteSessionAutoRefresh)

	window := newSweepWindow()
	q := repo.New(ti.conn)

	rows, err := q.ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Len(t, rows, 1, "the credential must stay keepalive-eligible through the sibling issuer's live session")
	require.Equal(t, session.ID, rows[0].ID)
	require.Equal(t, authCtx.ActiveOrganizationID, rows[0].OrganizationID)

	candidate, err := q.GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(session.ID, authCtx.ActiveOrganizationID))
	require.NoError(t, err)
	require.Equal(t, session.ID, candidate.RemoteSession.ID)
}

func TestRefreshSweep_NoLiveBoundIssuerSessionNotEligible(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuerID := createRemoteIssuer(t, ctx, ti, "shared-sweep-none-issuer", "")
	provenance := createUserSessionIssuer(t, ctx, ti.conn, "shared-sweep-none-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, provenance.String(), "shared-sweep-none-client")
	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)

	subject := urn.NewUserSubject("subject-shared-sweep-none")
	session, err := repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   provenance,
		RemoteSessionClientID: clientUUID,
		AccessTokenEncrypted:  "access-ciphertext",
		AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted: conv.ToPGText("refresh-ciphertext"),
		Scopes:                []string{},
		AutoRefresh:           true,
	})
	require.NoError(t, err)
	require.NoError(t, repo.New(ti.conn).SetRemoteSessionUpdatedAt(ctx, repo.SetRemoteSessionUpdatedAtParams{
		ID:        session.ID,
		ProjectID: conv.ToNullUUID(*authCtx.ProjectID),
		UpdatedAt: conv.ToPGTimestamptz(time.Now().Add(-25 * time.Hour)),
	}))
	enableOrgAutoRefreshFeature(t, ctx, ti, authCtx.ActiveOrganizationID, productfeatures.FeatureRemoteSessionAutoRefresh)

	window := newSweepWindow()
	rows, err := repo.New(ti.conn).ClaimDueRemoteSessionRefreshCandidates(ctx, window.claimParams())
	require.NoError(t, err)
	require.Empty(t, rows, "with no bound issuer holding a live Gram session, the keepalive must not claim the credential")

	_, err = repo.New(ti.conn).GetDueRemoteSessionRefreshCandidate(ctx, window.candidateParams(session.ID, authCtx.ActiveOrganizationID))
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
