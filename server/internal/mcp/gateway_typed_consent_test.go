package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Where typed auth client resolution is enforced, a gateway connects the
// registrations its proxied members offer through typed auth clients, with or
// without an older association or a link to the gateway's sign-in, and
// nothing an older association alone names. A hosted member keeps signing in
// through its authorization server's client.
func TestEnforcedGatewayConnectFollowsTypedOffers(t *testing.T) {
	t.Parallel()
	const upstream = "https://typed-member.example.com/mcp"
	for _, tc := range []struct {
		name            string
		orgGateway      bool
		orgClient       bool
		resourceDisplay bool
		typed           bool
		associated      bool
		signInLink      bool
		hosted          bool
		wantTyped       bool
		wantErr         string
	}{
		{name: "typed offer alone", typed: true, resourceDisplay: true, wantTyped: true},
		{name: "organization gateway typed offer", orgGateway: true, typed: true, resourceDisplay: true, wantTyped: true},
		{name: "organization gateway organization client", orgGateway: true, orgClient: true, typed: true, signInLink: true, wantTyped: true},
		{name: "organization gateway older association only", orgGateway: true, associated: true, signInLink: true, wantErr: "unknown remote session client"},
		{name: "typed offer linked to the sign-in", typed: true, signInLink: true, wantTyped: true},
		{name: "typed offer with an older association", typed: true, associated: true, signInLink: true, wantTyped: true},
		{name: "older association without a typed offer", associated: true, signInLink: true, wantErr: "unknown remote session client"},
		{name: "hosted member's sign-in client", hosted: true, signInLink: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, fx, metaServerID := seedMetaConsentEndpoint(t, "typed-gw-"+uuid.NewString()[:8])
			_, err := productfeaturesrepo.New(fx.ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: fx.orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
			require.NoError(t, err)
			fx.ti.service.SetAuthClientPermission(func() bool { return true })

			if tc.orgGateway {
				fx.endpoint.RouteBase = mcproute.OrganizationGateway
				fx.endpoint.ProjectID = uuid.Nil
				state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
				require.NoError(t, err)
				state.Endpoint.RouteBase = fx.endpoint.RouteBase
				require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))
			}

			var signIns []uuid.UUID
			if tc.signInLink && !tc.orgClient {
				signIns = []uuid.UUID{fx.shared}
			}
			registration := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "typed-gw-"+uuid.NewString()[:8], "", signIns)
			if tc.orgClient {
				q := remotesessionsrepo.New(fx.ti.conn)
				client, err := q.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
					OrganizationID:        conv.ToPGText(fx.orgID),
					RemoteSessionIssuerID: clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, registration),
					ClientID:              "org-client-" + uuid.NewString(),
				})
				require.NoError(t, err)
				registration = client.ID
				require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: registration, UserSessionIssuerID: fx.shared}))
			}
			member := createMetaMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "typed-gw-member-"+uuid.NewString()[:8], upstream, uuid.NullUUID{}, 0)
			seedPrincipalMCPConnectGrant(t, ctx, fx.ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, fx.subject.ID), member.mcpServerID)
			if tc.associated {
				_, err := upstreamclientsrepo.New(fx.ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: fx.orgID, ProjectID: fx.projectID, McpServerID: member.mcpServerID, RemoteSessionClientID: registration})
				require.NoError(t, err)
			}
			if tc.typed {
				fixtures := authclientfixtures.New(fx.ti.conn)
				authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{OrganizationID: fx.orgID, ProjectID: conv.ToNullUUID(fx.projectID), ClientType: "remote_session", Source: "admin", RemoteSessionClientID: conv.ToNullUUID(registration)})
				require.NoError(t, err)
				require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{ProjectID: fx.projectID, OrganizationID: fx.orgID, McpServerID: member.mcpServerID, AuthClientID: authClientID, Source: "admin"}))
			}
			if tc.hosted {
				hosted := seedHostedMetaMember(t, ctx, fx.ti, metaServerID, "Hosted", 1, "public")
				stampRemoteSessionIssuer(t, ctx, fx.ti.conn, fx.projectID, hosted.serverID, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, registration)))
			}

			if tc.resourceDisplay {
				registerPageClient(t, ctx, fx)
				_, err := remotesessionsrepo.New(fx.ti.conn).UpdateRemoteSessionClientResourceDisplay(ctx, remotesessionsrepo.UpdateRemoteSessionClientResourceDisplayParams{
					ID: registration, ProjectID: fx.projectID, OrganizationID: fx.orgID,
					ResourceIdentifier: conv.ToPGText(upstream), ResourceName: "Typed member resource",
					ResourcePolicyUri: "https://typed-member.example.com/policy",
				})
				require.NoError(t, err)
				code, html, _ := render(t, fx)
				require.Equal(t, http.StatusOK, code, html)
				require.Contains(t, html, "Typed member resource")
				require.Contains(t, html, `href="https://typed-member.example.com/policy"`)
			}
			if tc.orgClient {
				// No grant exists: validation must find the typed offer's project,
				// then report the missing credential rather than an unserved client.
				form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"validate"}, "client_id": {registration.String()}}
				req := httptest.NewRequest(http.MethodPost, "/mcp/gateway/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				w := httptest.NewRecorder()
				require.NoError(t, fx.ti.service.ServeConsentAction(w, req, fx.endpoint))
				require.Equal(t, http.StatusSeeOther, w.Code)
				require.Contains(t, w.Header().Get("Location"), "validate_failed="+registration.String())
			}

			if tc.wantErr != "" {
				form := url.Values{"state": {fx.stateID}, "csrf_token": {"csrf-token"}, "action": {"connect"}, "client_id": {registration.String()}}
				req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				w := httptest.NewRecorder()
				require.ErrorContains(t, fx.ti.service.ServeConsentAction(w, req, fx.endpoint), tc.wantErr)
				require.Empty(t, w.Header().Get("Location"))
				return
			}

			loc := postConnectAction(t, fx, registration)
			state := mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state"))
			if !tc.wantTyped {
				require.Nil(t, state.TypedAuthorization, "a hosted member signs in through its authorization server's client")
				return
			}
			require.Equal(t, upstream, loc.Query().Get("resource"), "the grant is qualified to the member offering the registration")
			require.NotNil(t, state.TypedAuthorization)
			require.Equal(t, fx.projectID, state.ProjectID)
			require.Equal(t, fx.endpoint.RouteBase, state.RouteBase)
			require.Equal(t, member.mcpServerID, state.TypedAuthorization.ServerID)
			require.Equal(t, registration, state.TypedAuthorization.RemoteSessionClientID)
		})
	}
}
