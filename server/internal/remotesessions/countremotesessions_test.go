package remotesessions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestCountRemoteSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-count", "")
	userIssuerAID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-count-a").String()
	userIssuerBID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-count-b").String()
	clientA := createRemoteClient(t, ctx, ti, issuerID, userIssuerAID, "rs-count-a")
	clientB := createRemoteClient(t, ctx, ti, issuerID, userIssuerBID, "rs-count-b")

	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_alice"), userIssuerAID, clientA)
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_bob"), userIssuerAID, clientA)
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_carol"), userIssuerBID, clientB)

	result, err := ti.service.CountRemoteSessions(ctx, &gen.CountRemoteSessionsPayload{
		RemoteSessionClientID: clientA,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Subjects)
}

func TestCountRemoteSessions_NoSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-count-empty", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-count-empty").String()
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "rs-count-empty")

	result, err := ti.service.CountRemoteSessions(ctx, &gen.CountRemoteSessionsPayload{
		RemoteSessionClientID: clientID,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), result.Subjects)
}

func TestCountRemoteSessions_InvalidClientID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CountRemoteSessions(ctx, &gen.CountRemoteSessionsPayload{
		RemoteSessionClientID: "not-a-uuid",
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCountRemoteSessions_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.CountRemoteSessions(ctx, &gen.CountRemoteSessionsPayload{
		RemoteSessionClientID: "00000000-0000-0000-0000-000000000000",
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}
