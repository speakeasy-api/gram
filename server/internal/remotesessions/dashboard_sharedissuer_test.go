package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Several MCP servers usually share one user session issuer, and a client's
// binding to that issuer is shared by all of them. Configuring one server's
// identity must leave the client another server signs in through where it is.
// sharedIssuer is two MCP servers on one user session issuer. The first is
// configured with its own client; the second is not configured yet.
type sharedIssuer struct {
	userIssuerID  uuid.UUID
	first         uuid.UUID
	firstProvider uuid.UUID
	firstClient   uuid.UUID
	second        uuid.UUID
}

func seedSharedIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug, registrationEndpoint string) sharedIssuer {
	t.Helper()
	return seedSharedIssuerOn(t, ctx, ti, slug, registrationEndpoint, createUserSessionIssuer(t, ctx, ti.conn, slug+"-issuer"))
}

func seedSharedIssuerOn(t *testing.T, ctx context.Context, ti *testInstance, slug, registrationEndpoint string, userIssuerID uuid.UUID) sharedIssuer {
	t.Helper()

	first, _ := sourceBackedTarget(t, ctx, ti, slug+"-first", userIssuerID)
	firstProvider := createServerIdentityProvider(t, ctx, ti, slug+"-first-provider", registrationEndpoint, false, []string{"none", "client_secret_basic"})
	result, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(first, firstProvider, slug+"-first-client"))
	require.NoError(t, err)
	firstClient, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)
	second, _ := sourceBackedTarget(t, ctx, ti, slug+"-second", userIssuerID)

	shared := sharedIssuer{userIssuerID: userIssuerID, first: first, firstProvider: firstProvider, firstClient: firstClient, second: second}
	requireFirstServerKeepsItsClient(t, ctx, ti, shared)
	return shared
}

// requireFirstServerKeepsItsClient checks everything the first server signs
// in through is as its own configuration left it: the binding, the server's
// association, the auth client derived from it and the derived provider.
func requireFirstServerKeepsItsClient(t *testing.T, ctx context.Context, ti *testInstance, shared sharedIssuer) {
	t.Helper()

	projectID := projectIDFromContext(t, ctx)
	requireClientBound(t, ctx, ti, shared.firstClient, shared.userIssuerID, true)
	require.Equal(t, []uuid.UUID{shared.firstClient}, associatedClients(t, ctx, ti.conn, projectID, shared.first))
	require.Equal(t,
		[]uuid.UUID{authclientbackfill.OAuthClientID(activeOrganizationID(t, ctx), shared.firstClient, authclientbackfill.TypeRemoteSession)},
		offeredAuthClients(t, ctx, ti.conn, shared.first),
	)
	require.Equal(t, conv.ToNullUUID(shared.firstProvider), storedIssuer(t, ctx, ti.conn, projectID, shared.first))
}

// offerOwnRegistration gives serverID an administrator's offer of a new
// registration of providerID, with no binding to any user session issuer and
// no association, and returns the registration and its auth client.
func offerOwnRegistration(t *testing.T, ctx context.Context, ti *testInstance, serverID, providerID uuid.UUID, clientID string) (uuid.UUID, uuid.UUID) {
	t.Helper()

	orgID := activeOrganizationID(t, ctx)
	projectID := projectIDFromContext(t, ctx)
	registrationID := createServerIdentityClient(t, ctx, ti, clientID, providerID, false)
	fixtures := authclientbackfill.New(ti.conn)
	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{
		OrganizationID:        orgID,
		ProjectID:             conv.ToNullUUID(projectID),
		ClientType:            authz.AuthClientTypeRemoteSession,
		Source:                "admin",
		RemoteSessionClientID: conv.ToNullUUID(registrationID),
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{
		ProjectID:      projectID,
		OrganizationID: orgID,
		McpServerID:    serverID,
		AuthClientID:   authClientID,
		Source:         "admin",
	}))
	return registrationID, authClientID
}

// Binding a client to an issuer is configuration for the issuer's one server,
// so it is offered there under enforcement as it is without.
func TestAttachUserSessionIssuerOffersOnTheSoleServerUnderEnforcement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := activeOrganizationID(t, ctx)
	enforceUpstreamAuth(t, ctx, ti.conn, orgID)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "attach-enforced-issuer")
	target, _ := sourceBackedTarget(t, ctx, ti, "attach-enforced-target", userIssuerID)
	providerID := createServerIdentityProvider(t, ctx, ti, "attach-enforced-provider", "", false, []string{"none"})
	clientID := createServerIdentityClient(t, ctx, ti, "attach-enforced-client", providerID, false)

	_, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{
		ID:                  clientID.String(),
		UserSessionIssuerID: userIssuerID.String(),
	})
	require.NoError(t, err)

	require.Equal(t, []uuid.UUID{clientID}, associatedClients(t, ctx, ti.conn, projectIDFromContext(t, ctx), target))
	require.Equal(t,
		[]uuid.UUID{authclientbackfill.OAuthClientID(orgID, clientID, authclientbackfill.TypeRemoteSession)},
		offeredAuthClients(t, ctx, ti.conn, target),
	)
}

// A client a deleted server configured stays bound to the issuer. It is not what
// the writers below configure, so it must not reach the server left on the
// issuer under enforcement.
func TestClientWritersLeaveADeletedServersClientOffTheSoleServerUnderEnforcement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// write changes the issuer's bindings and returns the client the
		// remaining server is expected to offer, if any.
		write func(t *testing.T, ctx context.Context, ti *testInstance, providerID, userIssuerID uuid.UUID) []uuid.UUID
	}{
		{name: "attaching a client", write: func(t *testing.T, ctx context.Context, ti *testInstance, providerID, userIssuerID uuid.UUID) []uuid.UUID {
			t.Helper()
			clientID := createServerIdentityClient(t, ctx, ti, "leftover-attached-client", providerID, false)
			_, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{ID: clientID.String(), UserSessionIssuerID: userIssuerID.String()})
			require.NoError(t, err)
			return []uuid.UUID{clientID}
		}},
		{name: "creating a client", write: func(t *testing.T, ctx context.Context, ti *testInstance, providerID, userIssuerID uuid.UUID) []uuid.UUID {
			t.Helper()
			created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
				RemoteSessionIssuerID: providerID.String(),
				UserSessionIssuerIds:  []string{userIssuerID.String()},
				ClientID:              "leftover-created-client",
				ClientSecret:          nil,
				SessionToken:          nil,
				ApikeyToken:           nil,
				ProjectSlugInput:      nil,
			})
			require.NoError(t, err)
			return []uuid.UUID{uuid.MustParse(created.ID)}
		}},
		{name: "detaching a client", write: func(t *testing.T, ctx context.Context, ti *testInstance, providerID, userIssuerID uuid.UUID) []uuid.UUID {
			t.Helper()
			clientID := createServerIdentityClient(t, ctx, ti, "leftover-detached-client", providerID, false)
			_, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{ID: clientID.String(), UserSessionIssuerID: userIssuerID.String()})
			require.NoError(t, err)
			_, err = ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{ID: clientID.String(), UserSessionIssuerID: userIssuerID.String()})
			require.NoError(t, err)
			return nil
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			orgID := activeOrganizationID(t, ctx)
			projectID := projectIDFromContext(t, ctx)
			enforceUpstreamAuth(t, ctx, ti.conn, orgID)
			shared := seedSharedIssuer(t, ctx, ti, "leftover", "")
			_, err := mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: shared.first, ProjectID: projectID})
			require.NoError(t, err)
			requireClientBound(t, ctx, ti, shared.firstClient, shared.userIssuerID, true)
			providerID := createServerIdentityProvider(t, ctx, ti, "leftover-second-provider", "", false, []string{"none"})

			want := tc.write(t, ctx, ti, providerID, shared.userIssuerID)

			require.ElementsMatch(t, want, associatedClients(t, ctx, ti.conn, projectID, shared.second))
			wantOffered := make([]uuid.UUID, 0, len(want))
			for _, clientID := range want {
				wantOffered = append(wantOffered, authclientbackfill.OAuthClientID(orgID, clientID, authclientbackfill.TypeRemoteSession))
			}
			require.ElementsMatch(t, wantOffered, offeredAuthClients(t, ctx, ti.conn, shared.second))
		})
	}
}

// Without enforcement CaptureSoleConsumers writes nothing, because Reconcile
// already captures there.
func TestCaptureSoleConsumersWritesNothingWithoutEnforcement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "capture-off-issuer")
	target, _ := sourceBackedTarget(t, ctx, ti, "capture-off-target", userIssuerID)
	providerID := createServerIdentityProvider(t, ctx, ti, "capture-off-provider", "", false, []string{"none"})
	clientID := createServerIdentityClient(t, ctx, ti, "capture-off-client", providerID, false)
	require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userIssuerID,
	}))

	require.NoError(t, upstreamclients.CaptureSoleConsumers(ctx, ti.conn, activeOrganizationID(t, ctx), projectID, []uuid.UUID{userIssuerID}, []uuid.UUID{clientID}))

	require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, target))
}

// A server an administrator configured apart from its issuer during
// enforcement keeps that offer after a rollback. A capture must not put the issuer's client beside it: that
// client would be offered there and its tokens could reach the server's upstream.
func TestCapturesLeaveAServerWithItsOwnOfferAloneAfterRollback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		issuer func(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID
		// deleteFirst leaves the second server as the issuer's only consumer,
		// which is the sole-consumer capture's case. Otherwise the server is
		// fronted by the organization's only organization-level issuer, which is
		// the gateway capture's.
		deleteFirst bool
	}{
		{name: "gateway capture", deleteFirst: false, issuer: func(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
			t.Helper()
			return seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "rollback-org-issuer")
		}},
		{name: "sole-consumer capture", deleteFirst: true, issuer: func(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
			t.Helper()
			return createUserSessionIssuer(t, ctx, ti.conn, "rollback-project-issuer")
		}},
	}
	for _, tc := range tests {
		for _, enabled := range []bool{true, false} {
			state := "disabled"
			if enabled {
				state = "enabled"
			}
			t.Run(tc.name+"/"+state, func(t *testing.T) {
				t.Parallel()

				ctx, ti := newTestService(t)
				orgID := activeOrganizationID(t, ctx)
				projectID := projectIDFromContext(t, ctx)
				enforceUpstreamAuth(t, ctx, ti.conn, orgID)
				shared := seedSharedIssuerOn(t, ctx, ti, "rollback", "", tc.issuer(t, ctx, ti))
				providerID := createServerIdentityProvider(t, ctx, ti, "rollback-second-provider", "", false, []string{"none"})
				secondClient, _ := offerOwnRegistration(t, ctx, ti, shared.second, providerID, "rollback-second-client")
				queries := authclientsrepo.New(ti.conn)
				listParams := authclientsrepo.ListServerAuthClientsParams{McpServerID: shared.second, ProjectID: projectID, OrganizationID: orgID}
				offers, err := queries.ListServerAuthClients(ctx, listParams)
				require.NoError(t, err)
				require.Len(t, offers, 1)
				if !enabled {
					_, err = queries.UpdateAuthClient(ctx, authclientsrepo.UpdateAuthClientParams{Name: offers[0].Name, Enabled: false, ID: offers[0].ID, OrganizationID: orgID})
					require.NoError(t, err)
				}
				_, err = productfeaturesrepo.New(ti.conn).DeleteFeature(ctx, productfeaturesrepo.DeleteFeatureParams{
					OrganizationID: orgID,
					FeatureName:    string(productfeatures.FeatureUpstreamAuthEnforced),
				})
				require.NoError(t, err)
				if tc.deleteFirst {
					_, err = mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: shared.first, ProjectID: projectID})
					require.NoError(t, err)
				}

				require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{shared.userIssuerID}))

				require.Equal(t, conv.ToNullUUID(shared.firstProvider), storedIssuer(t, ctx, ti.conn, projectID, shared.second))
				// Disabling an explicit registration is not consent to route the
				// issuer's different credentials to this server after rollback.
				requireClientBound(t, ctx, ti, secondClient, shared.userIssuerID, false)
				require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, shared.second))
				offers, err = queries.ListServerAuthClients(ctx, listParams)
				require.NoError(t, err)
				require.Len(t, offers, 1)
				require.Equal(t, conv.ToNullUUID(secondClient), offers[0].RemoteSessionClientID)
				require.Equal(t, authz.AuthClientTypeRemoteSession, offers[0].ClientType)
				require.Equal(t, "admin", offers[0].Source)
				require.Equal(t, enabled, offers[0].Enabled)
			})
		}
	}
}

// Under enforcement a client bound to the issuer of its one server is that
// server's configuration, whatever else the server already offers.
func TestAttachUserSessionIssuerOffersBesideTheServersOwnOfferUnderEnforcement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := activeOrganizationID(t, ctx)
	projectID := projectIDFromContext(t, ctx)
	enforceUpstreamAuth(t, ctx, ti.conn, orgID)
	shared := seedSharedIssuer(t, ctx, ti, "beside-offer", "")
	ownProvider := createServerIdentityProvider(t, ctx, ti, "beside-offer-own-provider", "", false, []string{"none"})
	offerOwnRegistration(t, ctx, ti, shared.second, ownProvider, "beside-offer-own-client")
	_, err := mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: shared.first, ProjectID: projectID})
	require.NoError(t, err)
	attachedProvider := createServerIdentityProvider(t, ctx, ti, "beside-offer-attached-provider", "", false, []string{"none"})
	attachedClient := createServerIdentityClient(t, ctx, ti, "beside-offer-attached-client", attachedProvider, false)

	_, err = ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{ID: attachedClient.String(), UserSessionIssuerID: shared.userIssuerID.String()})
	require.NoError(t, err)

	require.Equal(t, []uuid.UUID{attachedClient}, associatedClients(t, ctx, ti.conn, projectID, shared.second))
	require.Contains(t, offeredAuthClients(t, ctx, ti.conn, shared.second), authclientbackfill.OAuthClientID(orgID, attachedClient, authclientbackfill.TypeRemoteSession))
}

// An administrator's offer of a credential that is not an OAuth registration,
// such as a static credential, is configuration of its own: neither capture
// puts the issuer's client beside it after a rollback.
func TestCapturesLeaveAServerWithAStaticCredentialOfferAloneAfterRollback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		issuer      func(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID
		deleteFirst bool
	}{
		{name: "gateway capture", deleteFirst: false, issuer: func(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
			t.Helper()
			return seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "static-org-issuer")
		}},
		{name: "sole-consumer capture", deleteFirst: true, issuer: func(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
			t.Helper()
			return createUserSessionIssuer(t, ctx, ti.conn, "static-project-issuer")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			orgID := activeOrganizationID(t, ctx)
			projectID := projectIDFromContext(t, ctx)
			enforceUpstreamAuth(t, ctx, ti.conn, orgID)
			shared := seedSharedIssuerOn(t, ctx, ti, "static", "", tc.issuer(t, ctx, ti))
			fixtures := authclientbackfill.New(ti.conn)
			staticID, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{
				OrganizationID:        orgID,
				ProjectID:             conv.ToNullUUID(projectID),
				ClientType:            authz.AuthClientTypeStaticCredential,
				Source:                "admin",
				RemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			})
			require.NoError(t, err)
			require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{
				ProjectID:      projectID,
				OrganizationID: orgID,
				McpServerID:    shared.second,
				AuthClientID:   staticID,
				Source:         "admin",
			}))
			_, err = productfeaturesrepo.New(ti.conn).DeleteFeature(ctx, productfeaturesrepo.DeleteFeatureParams{
				OrganizationID: orgID,
				FeatureName:    string(productfeatures.FeatureUpstreamAuthEnforced),
			})
			require.NoError(t, err)
			if tc.deleteFirst {
				_, err = mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: shared.first, ProjectID: projectID})
				require.NoError(t, err)
			}

			require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{shared.userIssuerID}))

			require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, shared.second))
			require.Equal(t, []uuid.UUID{staticID}, offeredAuthClients(t, ctx, ti.conn, shared.second))
		})
	}
}
