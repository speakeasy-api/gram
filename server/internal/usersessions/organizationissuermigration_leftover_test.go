package usersessions_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Consolidating issuers whose servers already have typed clients creates no
// auth client: the servers keep the clients they already offer.
func TestOrganizationUserSessionIssuerMigrateCreatesNoAuthClientForTypedServers(t *testing.T) {
	t.Parallel()
	for _, enforced := range []bool{false, true} {
		t.Run(fmt.Sprintf("enforced=%t", enforced), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			leftoverEnableEnforcement(t, ctx, ti, enforced)
			targetID := seedIssuer(t, ctx, ti, "typed-target")
			sourceID := seedIssuer(t, ctx, ti, "typed-source")
			targetServer, _ := leftoverSeedServer(t, ctx, ti, targetID)
			sourceServer, _ := leftoverSeedServer(t, ctx, ti, sourceID)
			leftoverSyncServers(t, ctx, ti, targetServer, sourceServer)
			typed := authclientbackfill.New(ti.conn)
			before, err := typed.ListLiveAuthClientIDsFixture(ctx, auth.ActiveOrganizationID)
			require.NoError(t, err)
			require.Len(t, before, 2)

			_, err = ti.service.MigrateIssuer(ctx, confirmedMigratePayload(t, ctx, ti, sourceID.String(), targetID.String()))
			require.NoError(t, err)

			after, err := typed.ListLiveAuthClientIDsFixture(ctx, auth.ActiveOrganizationID)
			require.NoError(t, err)
			assert.Equal(t, before, after, "consolidation must not create an auth client for a server that already has one")
			for _, server := range []uuid.UUID{targetServer, sourceServer} {
				offered, err := typed.ListServerAuthClientsFixture(ctx, server)
				require.NoError(t, err)
				assert.Len(t, offered, 1, "each server keeps offering only its own client")
			}
		})
	}
}

// A server that moves onto a target with no other consumer keeps its offers.
// It does not inherit either issuer's compatibility parents.
func TestOrganizationUserSessionIssuerMigrateMovedSoleConsumerSkipsLeftoverSourceBinding(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	leftoverEnableEnforcement(t, ctx, ti, true)
	targetID := seedIssuer(t, ctx, ti, "moved-leftover-target")
	sourceID := seedIssuer(t, ctx, ti, "moved-leftover-source")
	movedServer, ownRegistration := leftoverSeedServer(t, ctx, ti, sourceID)
	leftoverSyncServers(t, ctx, ti, movedServer)
	leftoverSeedBoundRegistration(t, ctx, ti, targetID)
	leftoverSeedBoundRegistration(t, ctx, ti, sourceID)

	_, err := ti.service.MigrateIssuer(ctx, confirmedMigratePayload(t, ctx, ti, sourceID.String(), targetID.String()))
	require.NoError(t, err)

	registrations, err := authclientbackfill.New(ti.conn).ListOAuthClientsOfServers(ctx, authclientbackfill.ListOAuthClientsOfServersParams{OrganizationID: auth.ActiveOrganizationID, McpServerIds: []uuid.UUID{movedServer}})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{ownRegistration}, registrations)
}

func leftoverEnableEnforcement(t *testing.T, ctx context.Context, ti *testInstance, enforced bool) {
	t.Helper()
	if !enforced {
		return
	}
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: auth.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
}

// leftoverSeedBoundRegistration binds a registration to an issuer that no
// server is associated with, and returns the registration.
func leftoverSeedBoundRegistration(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) uuid.UUID {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	fixtures := selectionbackfill.New(ti.conn)
	providerID, clientID := uuid.New(), uuid.New()
	require.NoError(t, fixtures.SeedRemoteSessionIssuerFixture(ctx, selectionbackfill.SeedRemoteSessionIssuerFixtureParams{ID: providerID, ProjectID: conv.ToNullUUID(*auth.ProjectID), Slug: "provider-" + providerID.String(), Issuer: "https://" + providerID.String() + ".example.com"}))
	require.NoError(t, fixtures.SeedRemoteSessionClientFixture(ctx, selectionbackfill.SeedRemoteSessionClientFixtureParams{ID: clientID, ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), RemoteSessionIssuerID: providerID, ClientID: clientID.String()}))
	return clientID
}

// leftoverSeedServer seeds a proxied server signing in through the issuer and
// associated with one registration the issuer binds.
func leftoverSeedServer(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	fixtures := selectionbackfill.New(ti.conn)
	clientID := leftoverSeedBoundRegistration(t, ctx, ti, issuerID)
	backendID, serverID := uuid.New(), uuid.New()
	require.NoError(t, fixtures.SeedRemoteMCPServerFixture(ctx, selectionbackfill.SeedRemoteMCPServerFixtureParams{ID: backendID, ProjectID: *auth.ProjectID}))
	require.NoError(t, fixtures.SeedMCPServerFixture(ctx, selectionbackfill.SeedMCPServerFixtureParams{ID: serverID, ProjectID: *auth.ProjectID, Slug: conv.ToPGText("server-" + serverID.String()), RemoteMcpServerID: conv.ToNullUUID(backendID), UserSessionIssuerID: conv.ToNullUUID(issuerID)}))
	require.NoError(t, fixtures.SeedAssociationFixture(ctx, selectionbackfill.SeedAssociationFixtureParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, McpServerID: serverID, RemoteSessionClientID: clientID}))
	return serverID, clientID
}

func leftoverSyncServers(t *testing.T, ctx context.Context, ti *testInstance, servers ...uuid.UUID) {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, authclientbackfill.LockEMAWriterScope(ctx, tx, auth.ActiveOrganizationID, []uuid.UUID{*auth.ProjectID}))
	require.NoError(t, authclientbackfill.PreflightServerAssociations(ctx, tx, auth.ActiveOrganizationID, servers...))
	require.NoError(t, authclientbackfill.SyncServerAssociations(ctx, tx, auth.ActiveOrganizationID, servers...))
	require.NoError(t, tx.Commit(ctx))
}
