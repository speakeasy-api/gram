package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// Exercise the real consent action, cached state, callback validation, token
// exchange and credential persistence without a legacy issuer binding.
func TestTypedRemoteLoginConsentCallback(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"typed", "ema", "gateway", "gateway_ambiguous", "gateway_denied_member", "revoked_link", "deleted_auth_client", "disabled_auth_client", "revoked_permission", "revoked_connect", "deleted_server", "deleted_sign_in", "deleted_remote_issuer", "tenant_mismatch", "legacy_unlinked", "legacy"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			ti.service.manual = true
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			projectID, orgID := *auth.ProjectID, auth.ActiveOrganizationID
			issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
			if strings.HasPrefix(scenario, "gateway") {
				issuerID = createOrganizationIssuer(t, ctx, ti, orgID)
				setOrganizationGateway(t, ctx, ti, true)
			}
			var posted atomic.Value
			as := newConsentExchangeAS(t, &posted, "typed-upstream-token")
			var bindings []uuid.UUID
			if scenario == "legacy" {
				bindings = []uuid.UUID{issuerID}
			}
			registration := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "typed-login", as.URL, bindings)
			endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, issuerID, "typed-login")
			q := authclientbackfill.New(ti.conn)
			serverID, remoteID := uuid.New(), uuid.New()
			require.NoError(t, q.SeedRemoteMCPServerFixture(ctx, authclientbackfill.SeedRemoteMCPServerFixtureParams{ID: remoteID, ProjectID: projectID, Url: consentUpstreamA}))
			require.NoError(t, q.SeedMCPServerFixture(ctx, authclientbackfill.SeedMCPServerFixtureParams{ID: serverID, ProjectID: projectID, Slug: conv.ToPGText(endpoint.Slug), RemoteMcpServerID: conv.ToNullUUID(remoteID)}))
			endpoint.McpServerID = conv.ToNullUUID(serverID)
			endpoint.UpstreamResource = consentUpstreamA
			clientType := authz.AuthClientTypeRemoteSession
			if scenario == "ema" {
				clientType = authz.AuthClientTypeEMA
			}
			authClientID, err := q.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: orgID, ProjectID: conv.ToNullUUID(projectID), ClientType: clientType, Source: "admin", RemoteSessionClientID: conv.ToNullUUID(registration)})
			require.NoError(t, err)
			_, err = organizationsrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, organizationsrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: orgID, UserID: conv.ToPGText(subject.ID)})
			require.NoError(t, err)
			principal := urn.NewPrincipal(urn.PrincipalTypeUser, subject.ID)
			authGrantID := seedPrincipalGrant(t, ctx, ti, orgID, principal, authz.ScopeMCPAuth, authClientID.String())
			connectGrantID := seedPrincipalMCPConnectGrant(t, ctx, ti, orgID, principal, serverID)
			require.NoError(t, q.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: projectID, OrganizationID: orgID, McpServerID: serverID, AuthClientID: authClientID, Source: "admin"}))
			if scenario != "legacy" {
				ti.features.SetFlag(feature.FlagAuthClientResolver, orgID, true)
				_, err = productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
				require.NoError(t, err)
			}
			loaded, loadErr := ti.service.RemoteChallengeManager().ListAuthClientRegistrations(ctx, projectID, orgID, []uuid.UUID{registration})
			require.NoError(t, loadErr)
			require.Len(t, loaded, 1)
			if scenario == "legacy" {
				_, err = q.SeedAssociationFixture(ctx, authclientbackfill.SeedAssociationFixtureParams{ProjectID: projectID, OrganizationID: orgID, McpServerID: serverID, RemoteSessionClientID: registration})
				require.NoError(t, err)
			}
			if scenario == "typed" || scenario == "ema" {
				// The transactional binding check must reject a registration
				// deleted after the callback's earlier authorization reads.
				tx := testenv.BeginTx(t, ctx, ti.conn)
				locks := remotesessionsrepo.New(tx)
				params := remotesessionsrepo.LockTypedRemoteLoginAuthorizationParams{AuthClientID: authClientID, OrganizationID: orgID, ProjectID: projectID, RemoteSessionClientID: registration, McpServerID: serverID}
				_, err = locks.LockTypedRemoteLoginAuthorization(ctx, params)
				require.NoError(t, err)
				require.NoError(t, authclientbackfill.New(tx).SoftDeleteRemoteSessionClientFixture(ctx, registration))
				_, err = locks.LockTypedRemoteLoginAuthorization(ctx, params)
				require.ErrorIs(t, err, pgx.ErrNoRows)
				require.NoError(t, tx.Rollback(ctx))
			}
			fx := consentActionFixture{ti: ti, endpoint: endpoint, stateID: stateID, projectID: projectID, orgID: orgID, shared: issuerID, subject: subject}
			if scenario != "legacy" {
				require.NoError(t, ti.service.ValidateRemoteLoginTypedAuthorization(t.Context(), remotesessions.RemoteLoginState{OrganizationID: orgID, ProjectID: projectID, UserSessionIssuerID: issuerID, RemoteSessionClientID: registration, Subject: &subject, McpServerID: endpoint.McpServerID, TypedAuthorization: &remotesessions.TypedAuthorization{OrganizationID: orgID, ProjectID: projectID, ServerID: serverID, AuthClientID: authClientID, RemoteSessionClientID: registration, Subject: subject}}))
			}
			if strings.HasPrefix(scenario, "gateway") {
				endpoint.McpServerID = uuid.NullUUID{}
				endpoint.RouteBase = mcproute.OrganizationGateway
				endpoint.ProjectID = uuid.Nil
				endpoint.Slug = auth.OrganizationSlug
				challenge, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
				require.NoError(t, err)
				challenge.Endpoint.McpSlug = endpoint.Slug
				challenge.Endpoint.RouteBase = endpoint.RouteBase
				require.NoError(t, ti.authnChallengeCache.Store(ctx, challenge))
			}
			if scenario == "gateway_ambiguous" || scenario == "gateway_denied_member" {
				otherServer, otherRemote := uuid.New(), uuid.New()
				require.NoError(t, q.SeedRemoteMCPServerFixture(ctx, authclientbackfill.SeedRemoteMCPServerFixtureParams{ID: otherRemote, ProjectID: projectID, Url: consentUpstreamB}))
				require.NoError(t, q.SeedMCPServerFixture(ctx, authclientbackfill.SeedMCPServerFixtureParams{ID: otherServer, ProjectID: projectID, Slug: conv.ToPGText("second-typed-login"), RemoteMcpServerID: conv.ToNullUUID(otherRemote)}))
				otherClient, err := q.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: orgID, ProjectID: conv.ToNullUUID(projectID), ClientType: authz.AuthClientTypeRemoteSession, Source: "admin", RemoteSessionClientID: conv.ToNullUUID(registration)})
				require.NoError(t, err)
				seedPrincipalMCPConnectGrant(t, ctx, ti, orgID, principal, otherServer)
				require.NoError(t, q.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: projectID, OrganizationID: orgID, McpServerID: otherServer, AuthClientID: otherClient, Source: "admin"}))
				if scenario == "gateway_ambiguous" {
					seedPrincipalGrant(t, ctx, ti, orgID, principal, authz.ScopeMCPAuth, otherClient.String())
					form := url.Values{"state": {stateID}, "csrf_token": {"csrf-token"}, "action": {"connect"}, "client_id": {registration.String()}}
					req := httptest.NewRequest(http.MethodPost, "/mcp/"+endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					rec := recordConnections(t, ti)
					// A row needs a server id; Valid stays false to keep this fixture's typed path.
					gateway := uuid.NullUUID{UUID: uuid.New(), Valid: false}
					endpoint.MetaMcpServerID = gateway
					challenge, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
					require.NoError(t, err)
					challenge.Endpoint.MetaMcpServerID = gateway
					require.NoError(t, ti.authnChallengeCache.Store(ctx, challenge))
					err = ti.service.ServeConsentAction(httptest.NewRecorder(), req, endpoint)
					require.Error(t, err)
					var oopsErr *oops.ShareableError
					require.ErrorAs(t, err, &oopsErr)
					require.Equal(t, oops.CodeForbidden, oopsErr.Code)
					require.Nil(t, posted.Load())
					rec.RequireEnded(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeConfigurationRequired)
					return
				}
			}
			loc := postConnectAction(t, fx, registration)
			state := mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state"))
			if scenario == "legacy" {
				require.Nil(t, state.TypedAuthorization)
			} else {
				require.Equal(t, &remotesessions.TypedAuthorization{OrganizationID: orgID, ProjectID: projectID, ServerID: serverID, AuthClientID: authClientID, RemoteSessionClientID: registration, Subject: subject}, state.TypedAuthorization)
			}
			switch scenario {
			case "revoked_link":
				require.NoError(t, q.SoftDeleteServerAuthClientLinksFixture(ctx, serverID))
			case "deleted_auth_client":
				require.NoError(t, q.SoftDeleteAuthClientFixture(ctx, authClientID))
			case "disabled_auth_client":
				require.NoError(t, q.SetAuthClientEnabledFixture(ctx, authclientbackfill.SetAuthClientEnabledFixtureParams{ID: authClientID, Enabled: false}))
			case "deleted_server":
				require.NoError(t, q.SoftDeleteMCPServerFixture(ctx, serverID))
			case "revoked_permission":
				_, err = accessrepo.New(ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{ID: authGrantID, OrganizationID: orgID})
				require.NoError(t, err)
			case "revoked_connect":
				_, err = accessrepo.New(ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{ID: connectGrantID, OrganizationID: orgID})
				require.NoError(t, err)
			case "deleted_remote_issuer":
				require.NoError(t, q.SoftDeleteIssuerOfClientFixture(ctx, registration))
			case "deleted_sign_in":
				_, err = usersessionsrepo.New(ti.conn).DeleteUserSessionIssuer(ctx, usersessionsrepo.DeleteUserSessionIssuerParams{ID: issuerID, ProjectID: projectID})
				require.NoError(t, err)
			case "tenant_mismatch":
				state.TypedAuthorization.ProjectID = uuid.New()
			case "legacy_unlinked":
				state.TypedAuthorization = nil
			}
			if scenario == "tenant_mismatch" || scenario == "legacy_unlinked" {
				// Mutate trusted cached provenance to exercise compatibility and tamper checks.
				loginCache := cache.NewTypedObjectCache[remotesessions.RemoteLoginState](ti.logger, ti.cacheAdapter, cache.SuffixNone)
				require.NoError(t, loginCache.Store(ctx, state))
			}
			mgr := ti.service.RemoteChallengeManager()
			req := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?state="+url.QueryEscape(loc.Query().Get("state"))+"&code=fake-code", nil)
			w := httptest.NewRecorder()
			callbackErr := mgr.HandleRemoteLoginCallback(w, req)
			session, sessionErr := remotesessionsrepo.New(ti.conn).GetActiveRemoteSession(ctx, remotesessionsrepo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: registration})
			if scenario != "typed" && scenario != "ema" && scenario != "gateway" && scenario != "gateway_denied_member" && scenario != "legacy" {
				require.Error(t, callbackErr)
				if scenario != "legacy_unlinked" && scenario != "deleted_sign_in" {
					require.Nil(t, posted.Load(), "revocation must fail before exchanging the code")
				}
				require.ErrorIs(t, sessionErr, pgx.ErrNoRows)
				return
			}
			require.NoError(t, callbackErr)
			require.Equal(t, http.StatusSeeOther, w.Code)
			require.NotNil(t, posted.Load())
			require.NoError(t, sessionErr)
			require.Equal(t, state.Resource, session.Resource.String)
			if scenario == "typed" || scenario == "ema" || strings.HasPrefix(scenario, "gateway") {
				bound, err := mgr.ListClients(ctx, projectID, orgID, issuerID)
				require.NoError(t, err)
				require.Empty(t, bound, "typed login must not create a legacy issuer binding")
			}
			replay := httptest.NewRecorder()
			require.Error(t, mgr.HandleRemoteLoginCallback(replay, req.Clone(t.Context())), "callback state must be single use")
		})
	}
}
