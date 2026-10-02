package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
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

func TestCountRemoteSessions_HidesOtherProjectsClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	userIssuer := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "rs-count-org-issuer")
	remoteIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "rs-count-org-remote")
	otherProject := createProject(t, ctx, ti.conn, "rs-count-other")
	otherClient := seedRemoteClientAtTier(t, ctx, ti.conn, conv.ToNullUUID(otherProject), conv.ToPGText(authCtx.ActiveOrganizationID), remoteIssuer, "rs-count-sibling", userIssuer)
	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("rs_count_theirs"), userIssuer.String(), otherClient.String())

	// A sibling project's client is not this project's to count, even though
	// its session sits under an organization-tier issuer this project shares.
	result, err := ti.service.CountRemoteSessions(ctx, &gen.CountRemoteSessionsPayload{
		RemoteSessionClientID: otherClient.String(),
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), result.Subjects)
}

func TestCountRemoteSessions_CountsOnlyPeople(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "rs-count-people", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-rs-count-people").String()
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID, "rs-count-people")

	insertRemoteSession(t, ctx, ti.conn, urn.NewUserSubject("user_dana"), userIssuerID, clientID)
	insertRemoteSession(t, ctx, ti.conn, urn.NewAPIKeySubject(uuid.New()), userIssuerID, clientID)

	result, err := ti.service.CountRemoteSessions(ctx, &gen.CountRemoteSessionsPayload{
		RemoteSessionClientID: clientID,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Subjects)
}
