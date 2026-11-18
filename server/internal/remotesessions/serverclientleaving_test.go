package remotesessions_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// requireServerRequires asserts that the management API reports exactly the given
// clients and that the runtime agrees: callers with no connection are refused
// when there are any, and pass with no credential when there are none.
func requireServerRequires(t *testing.T, ctx context.Context, ti *testInstance, manager *remotesessions.ChallengeManager, server, issuer uuid.UUID, clients ...uuid.UUID) {
	t.Helper()
	listed, err := ti.service.ListServerClientAssociations(ctx, &gen.ListServerClientAssociationsPayload{McpServerID: server.String()})
	require.NoError(t, err)
	want := make([]string, 0, len(clients))
	for _, client := range clients {
		want = append(want, client.String())
	}
	require.ElementsMatch(t, want, listed.RequiredClientIds)

	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	resolved, err := manager.ResolveServerAccessTokens(ctx, remotesessions.RefreshAttribution{}, *auth.ProjectID, auth.ActiveOrganizationID, issuer, urn.NewUserSubject(auth.UserID), server, false)
	if len(clients) == 0 {
		require.NoError(t, err)
		require.Empty(t, resolved.Tokens)
		return
	}
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
}

func detachServerClient(ctx context.Context, ti *testInstance, server, client uuid.UUID) error {
	if err := ti.service.DetachServerClientAssociation(ctx, &gen.DetachServerClientAssociationPayload{
		McpServerID:           server.String(),
		RemoteSessionClientID: client.String(),
	}); err != nil {
		return fmt.Errorf("detach server client: %w", err)
	}
	return nil
}

func associateServerClient(t *testing.T, ctx context.Context, ti *testInstance, server, client uuid.UUID) {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rows, err := upstreamclientsrepo.New(ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
		OrganizationID:        auth.ActiveOrganizationID,
		RemoteSessionClientID: client,
		McpServerID:           server,
		ProjectID:             *auth.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
}

// With only a gateway sharing the issuer no other server claims the client, so
// without the record of the detach the server would fall back to requiring it.
func TestLeavingStopsRequiringAClientAGatewayKeepsBound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	manager := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	target, issuer := createGatewayTarget(t, ctx, ti, "leaving-gateway-stops-target")
	createImpactGateway(t, ctx, ti, "leaving-gateway-stops", issuer)
	provider := createServerIdentityProvider(t, ctx, ti, "leaving-gateway-stops-provider", "", false, []string{"none"})
	client := commitManualClient(t, ctx, ti, target, provider, "leaving-gateway-stops-client")
	requireServerRequires(t, ctx, ti, manager, target, issuer, client)

	preview, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(issuer, &target, "detach", nil, nil))
	require.NoError(t, err)
	require.Equal(t, []string{client.String()}, preview.ReleaseClientIds)
	require.NoError(t, detachServerClient(ctx, ti, target, client))

	requireServerRequires(t, ctx, ti, manager, target, issuer)
	requireClientBound(t, ctx, ti, client, issuer, true)
}

// Servers sharing a client nobody has an association with each fall back to it,
// so one leaving keeps the client bound and only stops requiring it itself.
func TestLeavingAnUnclaimedClientOnlyAffectsThisServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	manager := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	issuer := createUserSessionIssuer(t, ctx, ti.conn, "leaving-unclaimed-issuer")
	client, _ := seedActiveClient(t, ctx, ti.conn, *auth.ProjectID, issuer, auth.ActiveOrganizationID, "leaving-unclaimed")
	first := createServerIdentityTargetOnIssuer(t, ctx, ti, "leaving-unclaimed-first", issuer)
	second := createServerIdentityTargetOnIssuer(t, ctx, ti, "leaving-unclaimed-second", issuer)
	requireServerRequires(t, ctx, ti, manager, first, issuer, client)
	requireServerRequires(t, ctx, ti, manager, second, issuer, client)

	preview, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(issuer, &first, "detach", nil, nil))
	require.NoError(t, err)
	require.Empty(t, preview.DetachClientIds)
	require.Equal(t, []string{client.String()}, preview.ReleaseClientIds)
	require.Empty(t, preview.Servers, "the other server keeps its sign-in")
	require.NoError(t, detachServerClient(ctx, ti, first, client))

	requireServerRequires(t, ctx, ti, manager, first, issuer)
	requireServerRequires(t, ctx, ti, manager, second, issuer, client)
	requireClientBound(t, ctx, ti, client, issuer, true)

	requireOopsCode(t, detachServerClient(ctx, ti, first, client), oops.CodeNotFound)
}

// Dropping a server's own client must not hand it the unclaimed one beside it.
func TestLeavingGivesUpUnclaimedClientsBesideTheServersOwn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	manager := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	issuer := createUserSessionIssuer(t, ctx, ti.conn, "leaving-beside-issuer")
	own, _ := seedActiveClient(t, ctx, ti.conn, *auth.ProjectID, issuer, auth.ActiveOrganizationID, "leaving-beside-own")
	unclaimed, _ := seedActiveClient(t, ctx, ti.conn, *auth.ProjectID, issuer, auth.ActiveOrganizationID, "leaving-beside-unclaimed")
	target := createServerIdentityTargetOnIssuer(t, ctx, ti, "leaving-beside-target", issuer)
	createServerIdentityTargetOnIssuer(t, ctx, ti, "leaving-beside-sibling", issuer)
	associateServerClient(t, ctx, ti, target, own)
	requireServerRequires(t, ctx, ti, manager, target, issuer, own)

	preview, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(issuer, &target, "detach", nil, nil))
	require.NoError(t, err)
	require.Equal(t, []string{own.String()}, preview.DetachClientIds)
	require.Equal(t, []string{unclaimed.String()}, preview.ReleaseClientIds)

	require.NoError(t, detachServerClient(ctx, ti, target, own))
	requireServerRequires(t, ctx, ti, manager, target, issuer, unclaimed)

	require.NoError(t, detachServerClient(ctx, ti, target, unclaimed))
	requireServerRequires(t, ctx, ti, manager, target, issuer)
}

// An explicit detach is a record of the past, not a refusal of the future.
func TestAssociatingAfterLeavingRequiresTheClientAgain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	manager := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	issuer := createUserSessionIssuer(t, ctx, ti.conn, "leaving-return-issuer")
	client, _ := seedActiveClient(t, ctx, ti.conn, *auth.ProjectID, issuer, auth.ActiveOrganizationID, "leaving-return")
	target := createServerIdentityTargetOnIssuer(t, ctx, ti, "leaving-return-target", issuer)
	createServerIdentityTargetOnIssuer(t, ctx, ti, "leaving-return-sibling", issuer)

	require.NoError(t, detachServerClient(ctx, ti, target, client))
	requireServerRequires(t, ctx, ti, manager, target, issuer)

	associateServerClient(t, ctx, ti, target, client)
	requireServerRequires(t, ctx, ti, manager, target, issuer, client)
}

// A server with no association can only give up a client it would fall back to
// needing, so a detach says nothing about any other client.
func TestDetachRefusesAClientTheServerDoesNotFallBackTo(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	manager := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	issuer := createUserSessionIssuer(t, ctx, ti.conn, "detach-refuses-issuer")
	claimed, _ := seedActiveClient(t, ctx, ti.conn, *auth.ProjectID, issuer, auth.ActiveOrganizationID, "detach-refuses-claimed")
	otherIssuer := createUserSessionIssuer(t, ctx, ti.conn, "detach-refuses-other-issuer")
	elsewhere, _ := seedActiveClient(t, ctx, ti.conn, *auth.ProjectID, otherIssuer, auth.ActiveOrganizationID, "detach-refuses-elsewhere")
	target := createServerIdentityTargetOnIssuer(t, ctx, ti, "detach-refuses-target", issuer)
	sibling := createServerIdentityTargetOnIssuer(t, ctx, ti, "detach-refuses-sibling", issuer)
	associateServerClient(t, ctx, ti, sibling, claimed)
	requireServerRequires(t, ctx, ti, manager, target, issuer)

	requireOopsCode(t, detachServerClient(ctx, ti, target, claimed), oops.CodeNotFound)
	requireOopsCode(t, detachServerClient(ctx, ti, target, elsewhere), oops.CodeNotFound)
	requireServerRequires(t, ctx, ti, manager, sibling, issuer, claimed)
}
