package remotesessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	orgsessionsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TestListClientSessions lists the sessions minted against a client.
func TestListClientSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-sessions-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-sessions-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-sessions-client")
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("admin-sessions-subject"), userIssuerID.String(), clientID)

	result, err := ti.service.ListClientSessions(ctx, &orgsessionsgen.ListClientSessionsPayload{
		ClientID:     clientID,
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, urn.NewUserSubject("admin-sessions-subject").String(), result.Items[0].SubjectUrn)
}

// TestRevokeSession revokes a single session and records an audit event.
func TestRevokeSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-revoke-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-revoke-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-revoke-client")
	session := insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("admin-revoke-subject"), userIssuerID.String(), clientID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionDelete)
	require.NoError(t, err)

	err = ti.service.RevokeSession(ctx, &orgsessionsgen.RevokeSessionPayload{
		ID:           session.ID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// The audit event is attributed to the client's owning project (resolved
	// from the revoked session's client), not left unattributed.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionDelete)
	require.NoError(t, err)
	require.True(t, record.ProjectID.Valid)
	require.Equal(t, *authCtx.ProjectID, record.ProjectID.UUID)

	// The session is gone from the client's active list.
	result, err := ti.service.ListClientSessions(ctx, &orgsessionsgen.ListClientSessionsPayload{
		ClientID:     clientID,
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Items)

	// Revoking again is idempotent.
	err = ti.service.RevokeSession(ctx, &orgsessionsgen.RevokeSessionPayload{
		ID:           session.ID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
}

// TestRevokeAllClientSessions revokes every session for a client and
// records exactly one bulk audit event with the revoked count.
func TestRevokeAllClientSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "admin-revokeall-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "admin-revokeall-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "admin-revokeall-client")
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("admin-revokeall-a"), userIssuerID.String(), clientID)
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("admin-revokeall-b"), userIssuerID.String(), clientID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientRevokeSessions)
	require.NoError(t, err)

	result, err := ti.service.RevokeAllClientSessions(ctx, &orgsessionsgen.RevokeAllClientSessionsPayload{
		ClientID:     clientID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.RevokedCount)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientRevokeSessions)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}
