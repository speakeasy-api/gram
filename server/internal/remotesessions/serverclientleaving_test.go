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

// A bound client no server is associated with is required by none of them, so
// a server leaving neither releases it nor unbinds it.
func TestLeavingLeavesAClientNoServerIsAssociatedWith(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	manager := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	issuer := createUserSessionIssuer(t, ctx, ti.conn, "leaving-unassociated-issuer")
	client, _ := seedActiveClient(t, ctx, ti.conn, *auth.ProjectID, issuer, auth.ActiveOrganizationID, "leaving-unassociated")
	first := createServerIdentityTargetOnIssuer(t, ctx, ti, "leaving-unassociated-first", issuer)
	second := createServerIdentityTargetOnIssuer(t, ctx, ti, "leaving-unassociated-second", issuer)
	requireServerRequires(t, ctx, ti, manager, first, issuer)
	requireServerRequires(t, ctx, ti, manager, second, issuer)

	preview, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(issuer, &first, "detach", nil, nil))
	require.NoError(t, err)
	require.Empty(t, preview.DetachClientIds)
	require.Empty(t, preview.ReleaseClientIds)
	require.Empty(t, preview.Servers, "the other server keeps its sign-in")

	requireOopsCode(t, detachServerClient(ctx, ti, first, client), oops.CodeNotFound)
	requireClientBound(t, ctx, ti, client, issuer, true)
}

// Dropping a server's own client must not hand it another client bound beside it.
func TestDetachingTheServersOwnClientRequiresNoOtherBoundClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	manager := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	issuer := createUserSessionIssuer(t, ctx, ti.conn, "leaving-beside-issuer")
	own, _ := seedActiveClient(t, ctx, ti.conn, *auth.ProjectID, issuer, auth.ActiveOrganizationID, "leaving-beside-own")
	beside, _ := seedActiveClient(t, ctx, ti.conn, *auth.ProjectID, issuer, auth.ActiveOrganizationID, "leaving-beside-unassociated")
	target := createServerIdentityTargetOnIssuer(t, ctx, ti, "leaving-beside-target", issuer)
	createServerIdentityTargetOnIssuer(t, ctx, ti, "leaving-beside-sibling", issuer)
	associateServerClient(t, ctx, ti, target, own)
	requireServerRequires(t, ctx, ti, manager, target, issuer, own)

	preview, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(issuer, &target, "detach", nil, nil))
	require.NoError(t, err)
	require.Equal(t, []string{own.String()}, preview.DetachClientIds)
	require.Empty(t, preview.ReleaseClientIds)

	require.NoError(t, detachServerClient(ctx, ti, target, own))
	requireServerRequires(t, ctx, ti, manager, target, issuer)
	requireOopsCode(t, detachServerClient(ctx, ti, target, beside), oops.CodeNotFound)
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
	associateServerClient(t, ctx, ti, target, client)

	require.NoError(t, detachServerClient(ctx, ti, target, client))
	requireServerRequires(t, ctx, ti, manager, target, issuer)

	associateServerClient(t, ctx, ti, target, client)
	requireServerRequires(t, ctx, ti, manager, target, issuer, client)
}

// Detach removes an association, so a client the server is not associated with
// has nothing to detach, whether another server claims it or it is bound elsewhere.
func TestDetachRefusesAClientTheServerIsNotAssociatedWith(t *testing.T) {
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
