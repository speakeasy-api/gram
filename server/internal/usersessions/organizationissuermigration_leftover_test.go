package usersessions_test

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/associationbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
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

// A registration still bound to a source issuer that no live server uses was
// left behind by earlier configuration. Consolidation leaves that binding
// behind, enforced or not, and the preview lists it as dropped: carried to the
// target, it would become a client the target's only server serves from
// without anyone having configured it there. Without a leftover binding the
// same consolidation changes nothing about that server.
func TestOrganizationUserSessionIssuerMigrateLeavesLeftoverSourceBindingBehind(t *testing.T) {
	t.Parallel()
	for _, enforced := range []bool{false, true} {
		for _, leftover := range []bool{false, true} {
			t.Run(fmt.Sprintf("enforced=%t/leftover=%t", enforced, leftover), func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestService(t)
				auth, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				leftoverEnableEnforcement(t, ctx, ti, enforced)
				targetID := seedIssuer(t, ctx, ti, "leftover-target")
				sourceID := seedIssuer(t, ctx, ti, "leftover-source")
				targetServer, targetRegistration := leftoverSeedServer(t, ctx, ti, targetID)
				leftoverSyncServers(t, ctx, ti, targetServer)
				var stale uuid.UUID
				if leftover {
					stale = leftoverSeedBoundRegistration(t, ctx, ti, sourceID)
				}
				typed := authclientbackfill.New(ti.conn)
				clientsBefore, err := typed.ListLiveAuthClientIDsFixture(ctx, auth.ActiveOrganizationID)
				require.NoError(t, err)
				require.Len(t, clientsBefore, 1)

				preflight, err := ti.service.GetIssuerMigratePreflight(ctx, &orggen.GetIssuerMigratePreflightPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
				require.NoError(t, err)
				require.True(t, preflight.ImpactPreviewed)
				assert.Empty(t, preflight.ServerImpacts, "the target's server keeps the client it uses")
				if leftover {
					require.Len(t, preflight.DroppedClientLinks, 1)
					assert.Equal(t, stale.String(), preflight.DroppedClientLinks[0].RemoteSessionClientID)
					assert.NotEmpty(t, preflight.WarningsFingerprint, "a dropped link needs confirming")
				} else {
					assert.Empty(t, preflight.DroppedClientLinks)
					assert.Empty(t, preflight.WarningsFingerprint)
				}

				result, err := ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: targetID.String(), ConfirmedWarningsFingerprint: new(preflight.WarningsFingerprint)})
				require.NoError(t, err)
				assert.Len(t, result.DroppedClientLinks, len(preflight.DroppedClientLinks))

				registrations, err := typed.ListOAuthClientsOfServers(ctx, authclientbackfill.ListOAuthClientsOfServersParams{OrganizationID: auth.ActiveOrganizationID, McpServerIds: []uuid.UUID{targetServer}})
				require.NoError(t, err)
				assert.Equal(t, []uuid.UUID{targetRegistration}, registrations, "a server is associated only with what was configured for it")
				offered, err := typed.ListServerAuthClientsFixture(ctx, targetServer)
				require.NoError(t, err)
				assert.Len(t, offered, 1)
				clientsAfter, err := typed.ListLiveAuthClientIDsFixture(ctx, auth.ActiveOrganizationID)
				require.NoError(t, err)
				assert.Len(t, clientsAfter, 1)
				if leftover {
					bound, err := remotesessionsrepo.New(ti.conn).CountRemoteSessionClientUserSessionIssuerBindings(ctx, remotesessionsrepo.CountRemoteSessionClientUserSessionIssuerBindingsParams{RemoteSessionClientID: stale, UserSessionIssuerID: targetID})
					require.NoError(t, err)
					assert.Zero(t, bound, "the leftover binding stays behind with the source")
				}

				// A credential issued after consolidation takes the path issuance
				// takes, and still has one decidable client on the target's server.
				issued, err := seedUserSession(t, ctx, ti.conn, targetID, urn.NewUserSubject("leftover-issued"))
				require.NoError(t, err)
				credentialID := issued.ID
				if issued.CredentialID.Valid {
					credentialID = issued.CredentialID.UUID
				}
				_, err = usersessionsrepo.New(ti.conn).RecordUpstreamClientSelections(ctx, usersessionsrepo.RecordUpstreamClientSelectionsParams{
					UserSessionIssuerID: targetID, CredentialKind: upstreamclients.CredentialKindUserSession,
					CredentialID: credentialID, Source: upstreamclients.SelectionSourceIssuance,
					PickedMcpServerIds: []uuid.UUID{}, PickedClientIds: []uuid.UUID{},
				})
				require.NoError(t, err)
				choices, err := selectionbackfill.New(ti.conn).ListSelectionsFixture(ctx, selectionbackfill.ListSelectionsFixtureParams{CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: credentialID})
				require.NoError(t, err)
				if assert.Len(t, choices, 1, "a new sign-in still has one decidable client on the target's only server") {
					assert.Equal(t, targetRegistration, choices[0].RemoteSessionClientID.UUID)
				}

				report, err := associationbackfill.Run(ctx, ti.conn, associationbackfill.Options{OrganizationID: auth.ActiveOrganizationID, Apply: false, PageSize: 0})
				require.NoError(t, err)
				assert.Equal(t, report.Enforced, report.Decided)
				assert.Zero(t, report.Gaps)
			})
		}
	}
}

// A server that moves onto a target with no other consumer offers the clients
// the target bound, as the move configures. A binding only the source held for
// another server stays out of it.
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
	targetRegistration := leftoverSeedBoundRegistration(t, ctx, ti, targetID)
	leftoverSeedBoundRegistration(t, ctx, ti, sourceID)

	_, err := ti.service.MigrateIssuer(ctx, confirmedMigratePayload(t, ctx, ti, sourceID.String(), targetID.String()))
	require.NoError(t, err)

	want := []uuid.UUID{ownRegistration, targetRegistration}
	slices.SortFunc(want, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	registrations, err := authclientbackfill.New(ti.conn).ListOAuthClientsOfServers(ctx, authclientbackfill.ListOAuthClientsOfServersParams{OrganizationID: auth.ActiveOrganizationID, McpServerIds: []uuid.UUID{movedServer}})
	require.NoError(t, err)
	assert.Equal(t, want, registrations)
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
	require.NoError(t, fixtures.SeedClientBindingFixture(ctx, selectionbackfill.SeedClientBindingFixtureParams{RemoteSessionClientID: clientID, UserSessionIssuerID: issuerID}))
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
