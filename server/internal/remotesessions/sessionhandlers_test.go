package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestListRemoteSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-list", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-list").String()
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "rs-list-client")

	live := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_live"), userIssuerID, clientID)
	soft := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_soft"), userIssuerID, clientID)

	// Soft-delete one row directly so it must be excluded from the listing.
	_, err := repo.New(ti.conn).RevokeRemoteSession(ctx, repo.RevokeRemoteSessionParams{
		ID:        soft.ID,
		ProjectID: liveProjectID(t, ctx),
	})
	require.NoError(t, err)

	result, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
		PrincipalUrn:          nil,
		RemoteSessionClientID: nil,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	ids := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		require.Equal(t, clientID, item.RemoteSessionClientID)
		require.Equal(t, userIssuerID, item.UserSessionIssuerID)
		ids[item.ID] = true
	}
	require.True(t, ids[live.ID.String()], "live session must be returned")
	require.False(t, ids[soft.ID.String()], "soft-deleted session must be excluded")
}

func TestListRemoteSessions_FilteredByPrincipal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-list-princ", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-list-princ").String()
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "rs-list-princ-client")

	alice := urn.NewUserSubject("user_alice")
	insertRemoteSession(t, ctx, ti.conn, alice, userIssuerID, clientID)
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_bob"), userIssuerID, clientID)

	filter := alice.String()
	result, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
		PrincipalUrn:          &filter,
		RemoteSessionClientID: nil,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, filter, result.Items[0].PrincipalUrn)
}

func TestListRemoteSessions_FilteredByClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-list-client", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-list-client").String()
	clientA := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "rs-list-client-a")
	clientB := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "rs-list-client-b")

	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_in_a"), userIssuerID, clientA)
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_in_b"), userIssuerID, clientB)

	result, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
		PrincipalUrn:          nil,
		RemoteSessionClientID: &clientA,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, clientA, result.Items[0].RemoteSessionClientID)
}

func TestListRemoteSessions_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
		PrincipalUrn:          nil,
		RemoteSessionClientID: nil,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestRevokeRemoteSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-revoke", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-revoke").String()
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "rs-revoke-client")

	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_revoke"), userIssuerID, clientID)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionDelete)
	require.NoError(t, err)

	err = ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{
		ID:               session.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Subsequent list must omit the revoked row.
	result, err := ti.service.ListRemoteSessions(ctx, &gen.ListRemoteSessionsPayload{
		PrincipalUrn:          nil,
		RemoteSessionClientID: &clientID,
		Cursor:                nil,
		Limit:                 nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	for _, item := range result.Items {
		require.NotEqual(t, session.ID.String(), item.ID)
	}
}

func TestRevokeRemoteSession_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.RevokeRemoteSession(ctx, &gen.RevokeRemoteSessionPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func liveProjectID(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return *authCtx.ProjectID
}
