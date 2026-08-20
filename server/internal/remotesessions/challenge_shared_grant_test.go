// challenge_shared_grant_test.go is the regression guard for AIS-589: a
// remote_sessions row is keyed on (subject_urn, remote_session_client_id).
// user_session_issuer_id is provenance from INSERT and is never updated on
// conflict. Consent reads and writes that filtered on that column hid a live
// grant from a second MCP server whose issuer did not mint the row, while
// GetActiveRemoteSession (the runtime) still found it.
package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type sharedGrantFixture struct {
	ti        *testInstance
	mgr       *remotesessions.ChallengeManager
	projectID uuid.UUID
	issuerA   uuid.UUID
	issuerB   uuid.UUID
	clientID  uuid.UUID
	subject   urn.SessionSubject
	session   repo.RemoteSession
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
		ti:        ti,
		mgr:       newDisconnectChallengeManager(t, ti),
		projectID: *authCtx.ProjectID,
		issuerA:   issuerA,
		issuerB:   issuerB,
		clientID:  uuid.MustParse(created.ID),
		subject:   subject,
		session:   session,
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

func TestRemoteSessionStatuses_FindsGrantMintedByDifferentIssuer(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantAcrossIssuers(t)

	require.Equal(t, fx.issuerA, fx.session.UserSessionIssuerID)

	statuses, err := fx.mgr.RemoteSessionStatuses(ctx, fx.subject, fx.projectID)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, statuses[fx.clientID].Status)

	// A second Connect on issuer B refreshes tokens but must not rewrite
	// provenance. The consent page still has to see the live grant.
	reconnect := insertRemoteSession(t, ctx, fx.ti.conn, fx.subject, fx.issuerB.String(), fx.clientID.String())
	require.Equal(t, fx.session.ID, reconnect.ID)
	require.Equal(t, fx.issuerA, reconnect.UserSessionIssuerID)

	statuses, err = fx.mgr.RemoteSessionStatuses(ctx, fx.subject, fx.projectID)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, statuses[fx.clientID].Status)
}

func TestSetRemoteSessionAutoRefresh_FindsGrantMintedByDifferentIssuer(t *testing.T) {
	t.Parallel()

	ctx, fx := seedSharedGrantAcrossIssuers(t)

	require.False(t, fx.session.AutoRefresh)

	n, err := fx.mgr.SetRemoteSessionAutoRefresh(ctx, fx.subject, fx.projectID, fx.clientID, true)
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

	n, err := fx.mgr.DisconnectRemoteSession(ctx, fx.subject, fx.projectID, fx.clientID)
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

	creds, err := newTestUpstreamRevoker(t, fx.ti).SoftDeleteSubjectSessions(ctx, fx.ti.conn, fx.subject, fx.projectID)
	require.NoError(t, err)
	require.Len(t, creds, 1)
	require.Equal(t, fx.clientID, creds[0].RemoteSessionClientID)

	_, err = repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            fx.subject,
		RemoteSessionClientID: fx.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestRemoteSessionStatuses_DoesNotCrossProjects(t *testing.T) {
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

	homeStatuses, err := fx.mgr.RemoteSessionStatuses(ctx, fx.subject, fx.projectID)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, homeStatuses[fx.clientID].Status)
	_, leaked := homeStatuses[otherClient.ID]
	require.False(t, leaked)

	otherStatuses, err := fx.mgr.RemoteSessionStatuses(ctx, fx.subject, otherProject)
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

	creds, err := newTestUpstreamRevoker(t, fx.ti).SoftDeleteSubjectSessions(ctx, fx.ti.conn, fx.subject, fx.projectID)
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
