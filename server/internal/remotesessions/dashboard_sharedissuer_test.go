package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/associationbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
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

// requireOfferedOnServerOnly checks the server offers the registration as an
// auth client of its own and nothing else reaches it: no binding to the user
// session issuer and no association.
func requireOfferedOnServerOnly(t *testing.T, ctx context.Context, ti *testInstance, serverID, registrationID, userIssuerID uuid.UUID) {
	t.Helper()

	projectID := projectIDFromContext(t, ctx)
	requireClientBound(t, ctx, ti, registrationID, userIssuerID, false)
	require.Empty(t, associatedClients(t, ctx, ti.conn, projectID, serverID))
	offered, err := authclientsrepo.New(ti.conn).ListServerAuthClients(ctx, authclientsrepo.ListServerAuthClientsParams{
		McpServerID:    serverID,
		ProjectID:      projectID,
		OrganizationID: activeOrganizationID(t, ctx),
	})
	require.NoError(t, err)
	require.Len(t, offered, 1)
	require.Equal(t, conv.ToNullUUID(registrationID), offered[0].RemoteSessionClientID)
	require.Equal(t, authz.AuthClientTypeRemoteSession, offered[0].ClientType)
	require.Equal(t, "admin", offered[0].Source)
	require.True(t, offered[0].Enabled)
}

func TestCommitServerIdentityConfigurationLeavesAnotherServersClientUnderEnforcement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// sameProvider configures the second server with the first server's
		// provider rather than another one.
		sameProvider bool
	}{
		{name: "another provider", sameProvider: false},
		{name: "the same provider", sameProvider: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			enforceUpstreamAuth(t, ctx, ti.conn, activeOrganizationID(t, ctx))
			shared := seedSharedIssuer(t, ctx, ti, "enforced-shared", "")
			providerID := shared.firstProvider
			if !tc.sameProvider {
				providerID = createServerIdentityProvider(t, ctx, ti, "enforced-shared-second-provider", "", false, []string{"none"})
			}
			detachesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDetachUserSessionIssuer)
			require.NoError(t, err)

			result, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(shared.second, providerID, "enforced-shared-second-client"))
			require.NoError(t, err)
			require.Equal(t, "registered", *result.Status)
			secondClient, err := uuid.Parse(result.Client.ID)
			require.NoError(t, err)
			require.NotEqual(t, shared.firstClient, secondClient)

			requireOfferedOnServerOnly(t, ctx, ti, shared.second, secondClient, shared.userIssuerID)
			// The user session issuer model never saw the commit, so turning
			// enforcement off finds the first server as it was.
			requireFirstServerKeepsItsClient(t, ctx, ti, shared)
			detachesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDetachUserSessionIssuer)
			require.NoError(t, err)
			require.Equal(t, detachesBefore, detachesAfter)
		})
	}
}

// Agents attached through the first server's client are no reason to refuse a
// server that never touches that client.
func TestCommitServerIdentityConfigurationIgnoresAnotherServersAgentsUnderEnforcement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enforceUpstreamAuth(t, ctx, ti.conn, activeOrganizationID(t, ctx))
	shared := seedSharedIssuer(t, ctx, ti, "enforced-agents", "")
	seedLiveAgentBinding(t, ctx, ti, shared.firstClient, shared.userIssuerID)
	providerID := createServerIdentityProvider(t, ctx, ti, "enforced-agents-second-provider", "", false, []string{"none"})

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(shared.second, providerID, "enforced-agents-second-client"))
	require.NoError(t, err)
	secondClient, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)

	requireOfferedOnServerOnly(t, ctx, ti, shared.second, secondClient, shared.userIssuerID)
	requireFirstServerKeepsItsClient(t, ctx, ti, shared)
	requireLiveAgentBindings(t, ctx, ti, shared.firstClient, shared.userIssuerID)
}

// Bindings that name no single current provider cannot say which client a
// replacement would unbind. Under enforcement that is no reason to refuse.
func TestCommitServerIdentityConfigurationIgnoresAmbiguousBindingsUnderEnforcement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enforceUpstreamAuth(t, ctx, ti.conn, activeOrganizationID(t, ctx))
	shared := seedSharedIssuer(t, ctx, ti, "enforced-ambiguous", "")
	extraProvider := createServerIdentityProvider(t, ctx, ti, "enforced-ambiguous-extra-provider", "", false, []string{"none"})
	extraClient := bindServerIdentityClient(t, ctx, ti, "enforced-ambiguous-extra-client", extraProvider, shared.userIssuerID, false)
	providerID := createServerIdentityProvider(t, ctx, ti, "enforced-ambiguous-second-provider", "", false, []string{"none"})

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(shared.second, providerID, "enforced-ambiguous-second-client"))
	require.NoError(t, err)
	secondClient, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)

	requireOfferedOnServerOnly(t, ctx, ti, shared.second, secondClient, shared.userIssuerID)
	requireClientBound(t, ctx, ti, shared.firstClient, shared.userIssuerID, true)
	requireClientBound(t, ctx, ti, extraClient, shared.userIssuerID, true)
}

// Offering a stored client on the server twice leaves one offer.
func TestCommitServerIdentityConfigurationOffersStoredClientOnceUnderEnforcement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enforceUpstreamAuth(t, ctx, ti.conn, activeOrganizationID(t, ctx))
	shared := seedSharedIssuer(t, ctx, ti, "enforced-stored", "")
	providerID := createServerIdentityProvider(t, ctx, ti, "enforced-stored-second-provider", "", false, []string{"none"})
	storedClient := createServerIdentityClient(t, ctx, ti, "enforced-stored-second-client", providerID, false)

	for range 2 {
		result, err := ti.service.CommitServerIdentityConfiguration(ctx, existingServerIdentityPayload(shared.second, providerID, storedClient))
		require.NoError(t, err)
		require.Equal(t, "linked", *result.Status)
		requireOfferedOnServerOnly(t, ctx, ti, shared.second, storedClient, shared.userIssuerID)
	}
	requireFirstServerKeepsItsClient(t, ctx, ti, shared)
}

// A server that is its client's only user still replaces it on the user
// session issuer under enforcement, so both models keep describing it.
func TestCommitServerIdentityConfigurationReplacesTheServersOwnClientUnderEnforcement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := activeOrganizationID(t, ctx)
	projectID := projectIDFromContext(t, ctx)
	enforceUpstreamAuth(t, ctx, ti.conn, orgID)
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "enforced-own-issuer")
	target, _ := sourceBackedTarget(t, ctx, ti, "enforced-own-target", userIssuerID)
	// A second server shares the issuer without using the client.
	sourceBackedTarget(t, ctx, ti, "enforced-own-bystander", userIssuerID)
	providerID := createServerIdentityProvider(t, ctx, ti, "enforced-own-provider", "", false, []string{"none"})
	first, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(target, providerID, "enforced-own-first-client"))
	require.NoError(t, err)
	firstClient, err := uuid.Parse(first.Client.ID)
	require.NoError(t, err)

	second, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(target, providerID, "enforced-own-second-client"))
	require.NoError(t, err)
	secondClient, err := uuid.Parse(second.Client.ID)
	require.NoError(t, err)

	requireClientBound(t, ctx, ti, firstClient, userIssuerID, false)
	requireClientBound(t, ctx, ti, secondClient, userIssuerID, true)
	require.Equal(t, []uuid.UUID{secondClient}, associatedClients(t, ctx, ti.conn, projectID, target))
	require.Equal(t,
		[]uuid.UUID{authclientbackfill.OAuthClientID(orgID, secondClient, authclientbackfill.TypeRemoteSession)},
		offeredAuthClients(t, ctx, ti.conn, target),
	)
}

func TestCommitServerIdentityConfigurationRefusesTakingAnotherServersClientWithoutEnforcement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// sameProvider configures the second server with the first server's
		// provider rather than another one.
		sameProvider bool
	}{
		{name: "another provider", sameProvider: false},
		{name: "the same provider", sameProvider: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			var requests atomic.Int64
			registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			t.Cleanup(registrationServer.Close)
			shared := seedSharedIssuer(t, ctx, ti, "unenforced-shared", registrationServer.URL)
			providerID := shared.firstProvider
			if !tc.sameProvider {
				providerID = createServerIdentityProvider(t, ctx, ti, "unenforced-shared-second-provider", registrationServer.URL, false, []string{"client_secret_basic"})
			}

			_, err := ti.service.CommitServerIdentityConfiguration(ctx, autoServerIdentityPayload(shared.second, providerID))
			requireOopsCode(t, err, oops.CodeConflict)
			require.ErrorContains(t, err, "Another MCP server signs in through the client")
			require.Zero(t, requests.Load(), "the refusal must come before an upstream client is registered")

			requireFirstServerKeepsItsClient(t, ctx, ti, shared)
			require.Empty(t, associatedClients(t, ctx, ti.conn, projectIDFromContext(t, ctx), shared.second))
			require.Empty(t, offeredAuthClients(t, ctx, ti.conn, shared.second))
		})
	}
}

// With enforcement off, a commit made from the server that owns the client
// still replaces it for the whole user session issuer.
func TestCommitServerIdentityConfigurationReplacesTheServersOwnClientOnSharedIssuerWithoutEnforcement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	shared := seedSharedIssuer(t, ctx, ti, "unenforced-own", "")

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(shared.first, shared.firstProvider, "unenforced-own-next-client"))
	require.NoError(t, err)
	nextClient, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)

	requireClientBound(t, ctx, ti, shared.firstClient, shared.userIssuerID, false)
	requireClientBound(t, ctx, ti, nextClient, shared.userIssuerID, true)
	require.Equal(t, []uuid.UUID{nextClient}, associatedClients(t, ctx, ti.conn, projectIDFromContext(t, ctx), shared.first))
}

// The preview names what the commit will do: under enforcement a commit that
// offers its client on the server only affects no other server.
func TestGetServerIdentityImpactForAnotherServersClient(t *testing.T) {
	t.Parallel()

	t.Run("under enforcement", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		enforceUpstreamAuth(t, ctx, ti.conn, activeOrganizationID(t, ctx))
		shared := seedSharedIssuer(t, ctx, ti, "impact-enforced", "")
		providerID := createServerIdentityProvider(t, ctx, ti, "impact-enforced-second-provider", "", false, []string{"none"})

		result, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(shared.userIssuerID, &shared.second, "replace", &providerID, nil))
		require.NoError(t, err)
		require.Empty(t, impactsByServer(result))
	})

	t.Run("without enforcement", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		shared := seedSharedIssuer(t, ctx, ti, "impact-unenforced", "")
		providerID := createServerIdentityProvider(t, ctx, ti, "impact-unenforced-second-provider", "", false, []string{"none"})

		_, err := ti.service.GetServerIdentityImpact(ctx, impactPayload(shared.userIssuerID, &shared.second, "replace", &providerID, nil))
		requireOopsCode(t, err, oops.CodeConflict)
		require.ErrorContains(t, err, "Another MCP server signs in through the client")
	})
}

// A setup save refuses a shared issuer because a binding would repoint its
// other servers, which cannot happen under enforcement.
func TestSetupSaveOnSharedIssuerSucceedsUnderEnforcement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enforceUpstreamAuth(t, ctx, ti.conn, activeOrganizationID(t, ctx))
	shared := seedSharedIssuer(t, ctx, ti, "setup-enforced", "")
	providerID := createServerIdentityProvider(t, ctx, ti, "setup-enforced-second-provider", "", false, []string{"none"})
	payload := manualExistingProviderIdentityPayload(shared.second, providerID, "setup-enforced-second-client")
	payload.TargetServerOnly = new(true)

	result, err := ti.service.CommitServerIdentityConfiguration(ctx, payload)
	require.NoError(t, err)
	secondClient, err := uuid.Parse(result.Client.ID)
	require.NoError(t, err)

	requireOfferedOnServerOnly(t, ctx, ti, shared.second, secondClient, shared.userIssuerID)
	requireFirstServerKeepsItsClient(t, ctx, ti, shared)
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

// Enforcement can be switched off and on again, and the staff migration runs
// each time. It derives associations from the issuer's bindings, which say
// nothing about a server configured apart from them, whichever way the issuer
// reaches it.
func TestMigrationLeavesAServerOnlyOfferAlone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		issuer func(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID
	}{
		{name: "project issuer", issuer: func(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
			t.Helper()
			return createUserSessionIssuer(t, ctx, ti.conn, "migration-project-issuer")
		}},
		{name: "the organization's only organization-level issuer", issuer: func(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
			t.Helper()
			return seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "migration-org-issuer")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			orgID := activeOrganizationID(t, ctx)
			projectID := projectIDFromContext(t, ctx)
			enforceUpstreamAuth(t, ctx, ti.conn, orgID)
			shared := seedSharedIssuerOn(t, ctx, ti, "migration-shared", "", tc.issuer(t, ctx, ti))
			providerID := createServerIdentityProvider(t, ctx, ti, "migration-second-provider", "", false, []string{"none"})
			result, err := ti.service.CommitServerIdentityConfiguration(ctx, manualExistingProviderIdentityPayload(shared.second, providerID, "migration-second-client"))
			require.NoError(t, err)
			secondClient, err := uuid.Parse(result.Client.ID)
			require.NoError(t, err)
			// Stamps the first server's provider on the second, as the resync
			// after a server is created does.
			require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{shared.userIssuerID}))
			require.Equal(t, conv.ToNullUUID(shared.firstProvider), storedIssuer(t, ctx, ti.conn, projectID, shared.second))

			_, err = associationbackfill.Run(ctx, ti.conn, associationbackfill.Options{OrganizationID: orgID, Apply: true, PageSize: 0})
			require.NoError(t, err)
			migrateOrganization(t, ctx, ti)

			requireOfferedOnServerOnly(t, ctx, ti, shared.second, secondClient, shared.userIssuerID)
			requireFirstServerKeepsItsClient(t, ctx, ti, shared)
		})
	}
}
