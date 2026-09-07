// challenge_shared_grant_test.go is the regression guard for AIS-589: a
// remote_sessions row is keyed on (subject_urn, remote_session_client_id).
// user_session_issuer_id is provenance from INSERT and is never updated on
// conflict. Consent reads and writes must find that live grant from every
// MCP server bound to the client, including after the minting issuer is
// soft-deleted (ON DELETE CASCADE never fires on a soft delete).
package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type sharedGrantFixture struct {
	ti             *testInstance
	mgr            *remotesessions.ChallengeManager
	projectID      uuid.UUID
	organizationID string
	issuerA        uuid.UUID
	issuerB        uuid.UUID
	clientID       uuid.UUID
	subject        urn.SessionSubject
	session        repo.RemoteSession
}

func seedSharedGrantAcrossIssuers(t *testing.T) (context.Context, sharedGrantFixture) {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	remoteIssuerID := createRemoteIssuer(t, ctx, ti, "ais-589-upstream", "")
	issuerA := createUserSessionIssuer(t, ctx, ti.conn, "ais-589-usi-a")
	issuerB := createUserSessionIssuer(t, ctx, ti.conn, "ais-589-usi-b")

	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: remoteIssuerID,
		UserSessionIssuerIds:  []string{issuerA.String(), issuerB.String()},
		ClientID:              "ais-589-client",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	subject := urn.NewUserSubject("ais-589-subject")
	session := insertRemoteSession(t, ctx, ti.conn, subject, issuerA.String(), created.ID)

	return ctx, sharedGrantFixture{
		ti:             ti,
		mgr:            newDisconnectChallengeManager(t, ti),
		projectID:      *authCtx.ProjectID,
		organizationID: authCtx.ActiveOrganizationID,
		issuerA:        issuerA,
		issuerB:        issuerB,
		clientID:       uuid.MustParse(created.ID),
		subject:        subject,
		session:        session,
	}
}

func newTestUpstreamRevoker(t *testing.T, ti *testInstance) *remotesessions.UpstreamRevoker {
	t.Helper()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	return remotesessions.NewUpstreamRevoker(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		testenv.NewEncryptionClient(t),
		policy,
	)
}

func seedSharedGrantThenSoftDeleteMintingIssuer(t *testing.T) (context.Context, sharedGrantFixture) {
	t.Helper()

	ctx, fx := seedSharedGrantAcrossIssuers(t)
	err := testrepo.New(fx.ti.conn).ForceSoftDeleteUserSessionIssuer(ctx, testrepo.ForceSoftDeleteUserSessionIssuerParams{
		ID:        fx.issuerA,
		ProjectID: fx.projectID,
	})
	require.NoError(t, err)
	return ctx, fx
}

func listBoundClientIDs(t *testing.T, ctx context.Context, fx sharedGrantFixture, issuerID uuid.UUID) []uuid.UUID {
	t.Helper()

	clients, err := fx.mgr.ListClients(ctx, fx.projectID, fx.organizationID, issuerID)
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(clients))
	for _, c := range clients {
		ids = append(ids, c.ID)
	}
	return ids
}

func requireBoundClient(t *testing.T, ids []uuid.UUID, clientID uuid.UUID) {
	t.Helper()
	require.Contains(t, ids, clientID)
}

func requireUnboundClient(t *testing.T, ids []uuid.UUID, clientID uuid.UUID) {
	t.Helper()
	require.NotContains(t, ids, clientID)
}

func TestRemoteSessionStatuses_FindsGrantMintedByDifferentIssuer(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantAcrossIssuers(t)

	require.Equal(t, fx.issuerA, fx.session.UserSessionIssuerID)

	bound := listBoundClientIDs(t, ctx, fx, fx.issuerB)
	requireBoundClient(t, bound, fx.clientID)

	statuses, err := fx.mgr.RemoteSessionStatuses(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, statuses[fx.clientID].Status)

	// A second Connect on issuer B refreshes tokens but must not rewrite
	// provenance. The consent page still has to see the live grant.
	reconnect := insertRemoteSession(t, ctx, fx.ti.conn, fx.subject, fx.issuerB.String(), fx.clientID.String())
	require.Equal(t, fx.session.ID, reconnect.ID)
	require.Equal(t, fx.issuerA, reconnect.UserSessionIssuerID)

	statuses, err = fx.mgr.RemoteSessionStatuses(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, statuses[fx.clientID].Status)
}

func TestSetRemoteSessionAutoRefresh_FindsGrantMintedByDifferentIssuer(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantAcrossIssuers(t)

	require.False(t, fx.session.AutoRefresh)

	bound := listBoundClientIDs(t, ctx, fx, fx.issuerB)
	requireBoundClient(t, bound, fx.clientID)

	n, err := fx.mgr.SetRemoteSessionAutoRefresh(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB, fx.clientID, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	active, err := repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.NoError(t, err)
	require.True(t, active.AutoRefresh)
	require.Equal(t, fx.issuerA, active.UserSessionIssuerID)
}

func TestDisconnectRemoteSession_FindsGrantMintedByDifferentIssuer(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantAcrossIssuers(t)

	bound := listBoundClientIDs(t, ctx, fx, fx.issuerB)
	requireBoundClient(t, bound, fx.clientID)

	n, err := fx.mgr.DisconnectRemoteSession(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	_, err = repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestSoftDeleteSubjectSessions_FindsGrantMintedByDifferentIssuer(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantAcrossIssuers(t)

	require.Equal(t, fx.issuerA, fx.session.UserSessionIssuerID)

	creds, err := newTestUpstreamRevoker(t, fx.ti).SoftDeleteSubjectSessions(ctx, fx.ti.conn, fx.subject, fx.issuerB, fx.projectID, fx.organizationID)
	require.NoError(t, err)
	require.Len(t, creds, 1)
	require.Equal(t, fx.clientID, creds[0].RemoteSessionClientID)

	_, err = repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestRemoteSessionStatuses_FiltersToSuppliedClientIDs(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantAcrossIssuers(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherProject := createProject(t, ctx, fx.ti.conn, "ais-589-other")
	otherUserIssuer := createUserSessionIssuerInProject(t, ctx, fx.ti.conn, otherProject, "ais-589-usi-other")
	otherRemoteIssuer := createRemoteIssuerInProject(t, ctx, fx.ti.conn, otherProject, "ais-589-rsi-other")

	q := repo.New(fx.ti.conn)
	otherClient, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(otherProject),
		OrganizationID:        conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID: otherRemoteIssuer,
		ClientID:              "ais-589-other-client",
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now()),
	})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: otherClient.ID,
		UserSessionIssuerID:   otherUserIssuer,
	}))
	insertRemoteSession(t, ctx, fx.ti.conn, fx.subject, otherUserIssuer.String(), otherClient.ID.String())

	homeStatuses, err := fx.mgr.RemoteSessionStatuses(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerA)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, homeStatuses[fx.clientID].Status)
	_, leaked := homeStatuses[otherClient.ID]
	require.False(t, leaked)

	otherStatuses, err := fx.mgr.RemoteSessionStatuses(ctx, fx.subject, otherProject, authCtx.ActiveOrganizationID, otherUserIssuer)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, otherStatuses[otherClient.ID].Status)
	_, leaked = otherStatuses[fx.clientID]
	require.False(t, leaked)
}

func TestSoftDeleteSubjectSessions_DoesNotCrossProjects(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantAcrossIssuers(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherProject := createProject(t, ctx, fx.ti.conn, "ais-589-other-revoke")
	otherUserIssuer := createUserSessionIssuerInProject(t, ctx, fx.ti.conn, otherProject, "ais-589-usi-other-revoke")
	otherRemoteIssuer := createRemoteIssuerInProject(t, ctx, fx.ti.conn, otherProject, "ais-589-rsi-other-revoke")

	q := repo.New(fx.ti.conn)
	otherClient, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(otherProject),
		OrganizationID:        conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID: otherRemoteIssuer,
		ClientID:              "ais-589-other-revoke-client",
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now()),
	})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: otherClient.ID,
		UserSessionIssuerID:   otherUserIssuer,
	}))
	insertRemoteSession(t, ctx, fx.ti.conn, fx.subject, otherUserIssuer.String(), otherClient.ID.String())

	creds, err := newTestUpstreamRevoker(t, fx.ti).SoftDeleteSubjectSessions(ctx, fx.ti.conn, fx.subject, fx.session.UserSessionIssuerID, fx.projectID, fx.organizationID)
	require.NoError(t, err)
	require.Len(t, creds, 1)
	require.Equal(t, fx.clientID, creds[0].RemoteSessionClientID)

	_, err = q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	_, err = q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: otherClient.ID,
	})
	require.NoError(t, err)
}

func TestRemoteSessionStatuses_FindsGrantAfterMintingIssuerSoftDeleted(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantThenSoftDeleteMintingIssuer(t)

	_, err := repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.NoError(t, err)

	bound := listBoundClientIDs(t, ctx, fx, fx.issuerB)
	requireBoundClient(t, bound, fx.clientID)

	statuses, err := fx.mgr.RemoteSessionStatuses(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, statuses[fx.clientID].Status)
}

func TestSetRemoteSessionAutoRefresh_FindsGrantAfterMintingIssuerSoftDeleted(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantThenSoftDeleteMintingIssuer(t)

	bound := listBoundClientIDs(t, ctx, fx, fx.issuerB)
	requireBoundClient(t, bound, fx.clientID)

	n, err := fx.mgr.SetRemoteSessionAutoRefresh(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB, fx.clientID, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	active, err := repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.NoError(t, err)
	require.True(t, active.AutoRefresh)
}

func TestDisconnectRemoteSession_FindsGrantAfterMintingIssuerSoftDeleted(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantThenSoftDeleteMintingIssuer(t)

	bound := listBoundClientIDs(t, ctx, fx, fx.issuerB)
	requireBoundClient(t, bound, fx.clientID)

	n, err := fx.mgr.DisconnectRemoteSession(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	_, err = repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestSoftDeleteSubjectSessions_FindsGrantAfterMintingIssuerSoftDeleted(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantThenSoftDeleteMintingIssuer(t)

	// The revoke runs through the live sibling issuer: the minting issuer is
	// gone, so it is never the one a Gram session revoke arrives on.
	creds, err := newTestUpstreamRevoker(t, fx.ti).SoftDeleteSubjectSessions(ctx, fx.ti.conn, fx.subject, fx.issuerB, fx.projectID, fx.organizationID)
	require.NoError(t, err)
	require.Len(t, creds, 1)
	require.Equal(t, fx.clientID, creds[0].RemoteSessionClientID)

	_, err = repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestSoftDeleteSubjectSessions_RevokesGrantOnDetachedClient(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantAcrossIssuers(t)

	// Detaching the minting issuer leaves a live sibling binding, so no orphan
	// cascade fires and the upstream tokens stay alive. A revoke arriving on
	// the detached issuer must still destroy the grant it minted, or it
	// reports success while the upstream credential survives.
	detached, err := repo.New(fx.ti.conn).DetachRemoteSessionClientFromUserSessionIssuer(ctx, repo.DetachRemoteSessionClientFromUserSessionIssuerParams{
		RemoteSessionClientID: fx.clientID,
		UserSessionIssuerID:   fx.issuerA,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), detached)
	requireUnboundClient(t, listBoundClientIDs(t, ctx, fx, fx.issuerA), fx.clientID)

	creds, err := newTestUpstreamRevoker(t, fx.ti).SoftDeleteSubjectSessions(ctx, fx.ti.conn, fx.subject, fx.issuerA, fx.projectID, fx.organizationID)
	require.NoError(t, err)
	require.Len(t, creds, 1)
	require.Equal(t, fx.clientID, creds[0].RemoteSessionClientID)

	_, err = repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func seedOtherProjectClient(
	t *testing.T,
	ctx context.Context,
	fx sharedGrantFixture,
	slug string,
) uuid.UUID {
	t.Helper()

	otherProject := createProject(t, ctx, fx.ti.conn, slug+"-project")
	otherUserIssuer := createUserSessionIssuerInProject(t, ctx, fx.ti.conn, otherProject, slug+"-usi")
	otherRemoteIssuer := createRemoteIssuerInProject(t, ctx, fx.ti.conn, otherProject, slug+"-rsi")

	q := repo.New(fx.ti.conn)
	otherClient, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(otherProject),
		OrganizationID:        conv.ToPGTextEmpty(fx.organizationID),
		RemoteSessionIssuerID: otherRemoteIssuer,
		ClientID:              slug + "-client",
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now()),
	})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: otherClient.ID,
		UserSessionIssuerID:   otherUserIssuer,
	}))
	insertRemoteSession(t, ctx, fx.ti.conn, fx.subject, otherUserIssuer.String(), otherClient.ID.String())
	return otherClient.ID
}

func TestListClients_ResolvesSharedClientThroughSecondIssuer(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantAcrossIssuers(t)
	otherClientID := seedOtherProjectClient(t, ctx, fx, "ais-589-list-xproj")

	bound := listBoundClientIDs(t, ctx, fx, fx.issuerB)
	requireBoundClient(t, bound, fx.clientID)
	requireUnboundClient(t, bound, otherClientID)

	// ListClients is the allowlist ServeConsentAction re-resolves posted
	// client_id against. A UUID from another project is not returned, so a
	// crafted form posting it never reaches SetRemoteSessionAutoRefresh.

	statuses, err := fx.mgr.RemoteSessionStatuses(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, statuses[fx.clientID].Status)
	_, leaked := statuses[otherClientID]
	require.False(t, leaked)

	for _, clientID := range bound {
		n, err := fx.mgr.SetRemoteSessionAutoRefresh(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB, clientID, true)
		require.NoError(t, err)
		require.Equal(t, int64(1), n)
	}

	home, err := repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.NoError(t, err)
	require.True(t, home.AutoRefresh)

	other, err := repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: otherClientID,
	})
	require.NoError(t, err)
	require.False(t, other.AutoRefresh)
}

func TestListClients_ResolvesSharedClientAfterMintingIssuerSoftDeleted(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantThenSoftDeleteMintingIssuer(t)

	boundA := listBoundClientIDs(t, ctx, fx, fx.issuerA)
	require.Empty(t, boundA)

	boundB := listBoundClientIDs(t, ctx, fx, fx.issuerB)
	requireBoundClient(t, boundB, fx.clientID)

	statuses, err := fx.mgr.RemoteSessionStatuses(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, statuses[fx.clientID].Status)
}

func TestListClients_ResolvesSharedClientAfterLookupIssuerSoftDeleted(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantAcrossIssuers(t)
	err := testrepo.New(fx.ti.conn).ForceSoftDeleteUserSessionIssuer(ctx, testrepo.ForceSoftDeleteUserSessionIssuerParams{
		ID:        fx.issuerB,
		ProjectID: fx.projectID,
	})
	require.NoError(t, err)

	// Consent is rendered for a live endpoint issuer. A deleted lookup
	// issuer has no cards; the surviving minting issuer still lists the
	// client and can mutate the grant.
	boundB := listBoundClientIDs(t, ctx, fx, fx.issuerB)
	require.Empty(t, boundB)

	boundA := listBoundClientIDs(t, ctx, fx, fx.issuerA)
	requireBoundClient(t, boundA, fx.clientID)

	statuses, err := fx.mgr.RemoteSessionStatuses(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerA)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, statuses[fx.clientID].Status)

	n, err := fx.mgr.SetRemoteSessionAutoRefresh(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerA, fx.clientID, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	n, err = fx.mgr.DisconnectRemoteSession(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerA, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

func newSharedGrantRefreshHandler(refreshCount *atomic.Int64, accessToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("refresh_token") != "upstream-refresh" {
			http.Error(w, "unexpected grant", http.StatusBadRequest)
			return
		}
		refreshCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + accessToken + `","token_type":"Bearer","expires_in":7200,"refresh_token":"rotated-refresh"}`))
	}
}

func seedRefreshableSharedGrant(t *testing.T, slug string, handler http.HandlerFunc) (context.Context, sharedGrantFixture) {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	tokenServer := httptest.NewServer(handler)
	t.Cleanup(tokenServer.Close)

	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	mgr := remotesessions.NewChallengeManager(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		enc,
		policy,
		cache.NoopCache,
		mustURL(t, "http://localhost"),
	)

	q := repo.New(ti.conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              slug + "-rsi",
		Issuer:                            tokenServer.URL,
		AuthorizationEndpoint:             conv.ToPGText(tokenServer.URL + "/authorize"),
		TokenEndpoint:                     conv.ToPGText(tokenServer.URL + "/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	})
	require.NoError(t, err)

	issuerA := createUserSessionIssuer(t, ctx, ti.conn, slug+"-usi-a")
	issuerB := createUserSessionIssuer(t, ctx, ti.conn, slug+"-usi-b")
	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuer.ID.String(),
		UserSessionIssuerIds:  []string{issuerA.String(), issuerB.String()},
		ClientID:              slug + "-client",
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	clientID := uuid.MustParse(created.ID)

	accessEnc, err := enc.Encrypt([]byte("stale-access"))
	require.NoError(t, err)
	refreshEnc, err := enc.Encrypt([]byte("upstream-refresh"))
	require.NoError(t, err)
	refreshEncStr := refreshEnc
	subject := urn.NewUserSubject(slug + "-subject")
	session, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:             subject,
		UserSessionIssuerID:    issuerA,
		RemoteSessionClientID:  clientID,
		AccessTokenEncrypted:   accessEnc,
		AccessExpiresAt:        conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
		RefreshTokenEncrypted:  conv.PtrToPGText(&refreshEncStr),
		AuthorizationExpiresAt: conv.ToPGTimestamptz(time.Now().Add(24 * time.Hour)),
		RefreshExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(2 * time.Hour)),
		Scopes:                 []string{},
		Resource:               pgtype.Text{},
		AutoRefresh:            true,
	})
	require.NoError(t, err)

	return ctx, sharedGrantFixture{
		ti:             ti,
		mgr:            mgr,
		projectID:      *authCtx.ProjectID,
		organizationID: authCtx.ActiveOrganizationID,
		issuerA:        issuerA,
		issuerB:        issuerB,
		clientID:       clientID,
		subject:        subject,
		session:        session,
	}
}

// A session with no persisted resource must refresh with the client's own
// derived RFC 8707 resource, never an endpoint-level one.
func TestRefreshRemoteSession_DerivesPerClientFallbackResource(t *testing.T) {
	t.Parallel()

	var refreshCount atomic.Int64
	var captured atomic.Value
	ctx, fx := seedRefreshableSharedGrant(t, "age-3328-refresh-res", newResourceCapturingRefreshHandler(&refreshCount, &captured, "rotated-with-resource"))

	// The client's sole attached MCP server pins its derived resource.
	attachRemoteMcpServerToIssuer(t, ctx, fx.ti.conn, fx.projectID, fx.issuerA, "age-3328-refresh-res", "https://upstream-refresh.example.com/")

	result, err := fx.mgr.RefreshRemoteSession(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeRefreshed, result.Outcome)
	require.Equal(t, int64(1), refreshCount.Load())
	require.Equal(t, tokenPostCapture{HasResource: true, Resource: "https://upstream-refresh.example.com"}, captured.Load())
}

// The lazy request-time path must also derive per client, never an
// endpoint-level fallback (AGE-3328): a NULL-resource row refreshed via
// ResolveAccessTokens sends the client's own derived resource upstream.
func TestResolveAccessTokens_DerivesPerClientFallbackResource(t *testing.T) {
	t.Parallel()

	var refreshCount atomic.Int64
	var captured atomic.Value
	ctx, fx := seedRefreshableSharedGrant(t, "age-3328-lazy-res", newResourceCapturingRefreshHandler(&refreshCount, &captured, "rotated-lazy-resource"))

	attachRemoteMcpServerToIssuer(t, ctx, fx.ti.conn, fx.projectID, fx.issuerA, "age-3328-lazy-res", "https://upstream-lazy.example.com/")
	require.NoError(t, testrepo.New(fx.ti.conn).ExpireRemoteSessionAccessTokenFixture(ctx, fx.session.ID))

	tokens, err := fx.mgr.ResolveAccessTokens(ctx, fx.projectID, fx.organizationID, fx.issuerB, fx.subject)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Equal(t, int64(1), refreshCount.Load())
	require.Equal(t, tokenPostCapture{HasResource: true, Resource: "https://upstream-lazy.example.com"}, captured.Load())
}

// tokenPostCapture is the resource param of the last refresh POST; presence
// matters separately from value (RFC 8707 omits the param when absent).
type tokenPostCapture struct {
	HasResource bool
	Resource    string
}

// newResourceCapturingRefreshHandler wraps newSharedGrantRefreshHandler and
// records whether/what `resource` the refresh POST carried.
func newResourceCapturingRefreshHandler(refreshCount *atomic.Int64, captured *atomic.Value, accessToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/token" && r.ParseForm() == nil {
			_, has := r.PostForm["resource"]
			captured.Store(tokenPostCapture{HasResource: has, Resource: r.PostForm.Get("resource")})
		}
		newSharedGrantRefreshHandler(refreshCount, accessToken)(w, r)
	}
}

// setStoredSessionResource stamps a persisted RFC 8707 resource onto the
// fixture's session via the same upsert the code-exchange callback uses.
func setStoredSessionResource(t *testing.T, ctx context.Context, fx sharedGrantFixture, resource string) {
	t.Helper()
	_, err := repo.New(fx.ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:             fx.subject,
		UserSessionIssuerID:    fx.session.UserSessionIssuerID,
		RemoteSessionClientID:  fx.clientID,
		AccessTokenEncrypted:   fx.session.AccessTokenEncrypted,
		AccessExpiresAt:        fx.session.AccessExpiresAt,
		RefreshTokenEncrypted:  fx.session.RefreshTokenEncrypted,
		AuthorizationExpiresAt: fx.session.AuthorizationExpiresAt,
		RefreshExpiresAt:       fx.session.RefreshExpiresAt,
		Scopes:                 fx.session.Scopes,
		Resource:               conv.ToPGText(resource),
		AutoRefresh:            fx.session.AutoRefresh,
	})
	require.NoError(t, err)
}

// A persisted resource wins on explicit refresh: no derivation replaces it,
// even when the client's attached MCP server would derive something else.
func TestRefreshRemoteSession_StoredResourceWinsOverDerivation(t *testing.T) {
	t.Parallel()

	var refreshCount atomic.Int64
	var captured atomic.Value
	ctx, fx := seedRefreshableSharedGrant(t, "age-3328-stored-res", newResourceCapturingRefreshHandler(&refreshCount, &captured, "rotated-stored"))

	attachRemoteMcpServerToIssuer(t, ctx, fx.ti.conn, fx.projectID, fx.issuerA, "age-3328-stored-res", "https://derived-loser.example.com/")
	setStoredSessionResource(t, ctx, fx, "https://stored-winner.example.com")

	result, err := fx.mgr.RefreshRemoteSession(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeRefreshed, result.Outcome)
	require.Equal(t, tokenPostCapture{HasResource: true, Resource: "https://stored-winner.example.com"}, captured.Load())
}

// NULL stored resource + ambiguous derivation (two attached upstreams with
// distinct URLs) must POST no resource param at all on explicit refresh.
func TestRefreshRemoteSession_AmbiguousDerivationSendsNoResource(t *testing.T) {
	t.Parallel()

	var refreshCount atomic.Int64
	var captured atomic.Value
	ctx, fx := seedRefreshableSharedGrant(t, "age-3328-amb-refresh", newResourceCapturingRefreshHandler(&refreshCount, &captured, "rotated-ambiguous"))

	attachRemoteMcpServerToIssuer(t, ctx, fx.ti.conn, fx.projectID, fx.issuerA, "age-3328-amb-refresh-1", "https://amb-one.example.com")
	attachRemoteMcpServerToIssuer(t, ctx, fx.ti.conn, fx.projectID, fx.issuerB, "age-3328-amb-refresh-2", "https://amb-two.example.com")

	result, err := fx.mgr.RefreshRemoteSession(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeRefreshed, result.Outcome)
	require.Equal(t, tokenPostCapture{HasResource: false, Resource: ""}, captured.Load())
}

// The lazy request-time path uses a persisted resource as-is: derivation
// never overrides it and the resolved entry reports the stored value.
func TestResolveAccessTokens_StoredResourceWinsOverDerivation(t *testing.T) {
	t.Parallel()

	var refreshCount atomic.Int64
	var captured atomic.Value
	ctx, fx := seedRefreshableSharedGrant(t, "age-3328-lazy-stored", newResourceCapturingRefreshHandler(&refreshCount, &captured, "rotated-lazy-stored"))

	attachRemoteMcpServerToIssuer(t, ctx, fx.ti.conn, fx.projectID, fx.issuerA, "age-3328-lazy-stored", "https://derived-loser.example.com/")
	setStoredSessionResource(t, ctx, fx, "https://stored-lazy.example.com")
	require.NoError(t, testrepo.New(fx.ti.conn).ExpireRemoteSessionAccessTokenFixture(ctx, fx.session.ID))

	tokens, err := fx.mgr.ResolveAccessTokens(ctx, fx.projectID, fx.organizationID, fx.issuerB, fx.subject)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Equal(t, tokenPostCapture{HasResource: true, Resource: "https://stored-lazy.example.com"}, captured.Load())
	for _, tok := range tokens {
		require.Equal(t, "https://stored-lazy.example.com", tok.Resource)
	}
}

// Lazy path, NULL stored resource, ambiguous derivation: the refresh POST
// must carry no resource param.
func TestResolveAccessTokens_AmbiguousDerivationSendsNoResource(t *testing.T) {
	t.Parallel()

	var refreshCount atomic.Int64
	var captured atomic.Value
	ctx, fx := seedRefreshableSharedGrant(t, "age-3328-lazy-amb", newResourceCapturingRefreshHandler(&refreshCount, &captured, "rotated-lazy-ambiguous"))

	attachRemoteMcpServerToIssuer(t, ctx, fx.ti.conn, fx.projectID, fx.issuerA, "age-3328-lazy-amb-1", "https://amb-one.example.com")
	attachRemoteMcpServerToIssuer(t, ctx, fx.ti.conn, fx.projectID, fx.issuerB, "age-3328-lazy-amb-2", "https://amb-two.example.com")
	require.NoError(t, testrepo.New(fx.ti.conn).ExpireRemoteSessionAccessTokenFixture(ctx, fx.session.ID))

	tokens, err := fx.mgr.ResolveAccessTokens(ctx, fx.projectID, fx.organizationID, fx.issuerB, fx.subject)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Equal(t, tokenPostCapture{HasResource: false, Resource: ""}, captured.Load())
}

// A caller-supplied non-empty fallback (the platform ResolveAccessToken /
// ResolveAuthorization path) wins over derivation on a NULL-resource row.
func TestResolveAccessToken_ExplicitFallbackSkipsDerivation(t *testing.T) {
	t.Parallel()

	var refreshCount atomic.Int64
	var captured atomic.Value
	ctx, fx := seedRefreshableSharedGrant(t, "age-3328-explicit-fb", newResourceCapturingRefreshHandler(&refreshCount, &captured, "rotated-explicit-fb"))

	attachRemoteMcpServerToIssuer(t, ctx, fx.ti.conn, fx.projectID, fx.issuerA, "age-3328-explicit-fb", "https://derived-unused.example.com/")
	require.NoError(t, testrepo.New(fx.ti.conn).ExpireRemoteSessionAccessTokenFixture(ctx, fx.session.ID))

	token, err := fx.mgr.ResolveAccessToken(ctx, fx.clientID, fx.subject, "https://caller-fallback.example.com")
	require.NoError(t, err)
	require.Equal(t, "rotated-explicit-fb", token)
	require.Equal(t, tokenPostCapture{HasResource: true, Resource: "https://caller-fallback.example.com"}, captured.Load())
}

func TestRefreshRemoteSession_FindsGrantMintedByDifferentIssuer(t *testing.T) {
	t.Parallel()

	var refreshCount atomic.Int64
	ctx, fx := seedRefreshableSharedGrant(t, "ais-589-refresh-v1", newSharedGrantRefreshHandler(&refreshCount, "rotated-access"))

	bound := listBoundClientIDs(t, ctx, fx, fx.issuerB)
	requireBoundClient(t, bound, fx.clientID)

	result, err := fx.mgr.RefreshRemoteSession(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeRefreshed, result.Outcome)
	require.Equal(t, "rotated-access", result.AccessToken)
	require.Equal(t, fx.issuerA, result.Session.UserSessionIssuerID)
	require.Equal(t, int64(1), refreshCount.Load())
}

func TestRefreshRemoteSession_FindsGrantAfterMintingIssuerSoftDeleted(t *testing.T) {
	t.Parallel()

	var refreshCount atomic.Int64
	ctx, fx := seedRefreshableSharedGrant(t, "ais-589-refresh-v2", newSharedGrantRefreshHandler(&refreshCount, "rotated-after-delete"))
	err := testrepo.New(fx.ti.conn).ForceSoftDeleteUserSessionIssuer(ctx, testrepo.ForceSoftDeleteUserSessionIssuerParams{
		ID:        fx.issuerA,
		ProjectID: fx.projectID,
	})
	require.NoError(t, err)

	bound := listBoundClientIDs(t, ctx, fx, fx.issuerB)
	requireBoundClient(t, bound, fx.clientID)

	result, err := fx.mgr.RefreshRemoteSession(ctx, fx.subject, fx.projectID, fx.organizationID, fx.issuerB, fx.clientID)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.RefreshOutcomeRefreshed, result.Outcome)
	require.Equal(t, "rotated-after-delete", result.AccessToken)
	require.Equal(t, fx.issuerA, result.Session.UserSessionIssuerID)
	require.Equal(t, int64(1), refreshCount.Load())
}
