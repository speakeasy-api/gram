package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

// A configured offer is discoverable before a user connects it, without the
// legacy organization issuer binding. Rollback must restore legacy filtering.
func TestOrganizationGatewayTypedAuthEligibility(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, clientType, header, source, value string
		required                                bool
		pending, bound, detached                bool
		noLegacyAssociation                     bool
	}{
		{name: "detached_required_caller_header", detached: true, bound: true, clientType: authz.AuthClientTypeStaticCredential, header: "X-API-Key", source: "X-API-Key", required: true},
		{name: "detached_unbound_oauth", detached: true, clientType: authz.AuthClientTypeRemoteSession},
		{name: "detached_bound_oauth", detached: true, bound: true, clientType: authz.AuthClientTypeRemoteSession},
		{name: "legacy_pending_bound", pending: true, bound: true},
		{name: "legacy_pending_unbound", pending: true},
		{name: "personal_pending", clientType: authz.AuthClientTypeRemoteSession},
		{name: "shared_pending", clientType: authz.AuthClientTypeServiceAccount},
		{name: "static_offer", clientType: authz.AuthClientTypeStaticCredential},
		{name: "static_without_legacy_association", clientType: authz.AuthClientTypeStaticCredential, noLegacyAssociation: true},
		{name: "admin_header_override", clientType: authz.AuthClientTypeStaticCredential, header: "X-API-Key", source: "X-API-Key", required: true},
		{name: "authorization_from_token", clientType: authz.AuthClientTypeRemoteSession, header: "Authorization", source: "Authorization", required: true},
		{name: "required_caller_header", clientType: authz.AuthClientTypeRemoteSession, header: "X-API-Key", source: "X-API-Key", required: true},
		{name: "optional_caller_header", clientType: authz.AuthClientTypeRemoteSession, header: "X-API-Key", source: "X-API-Key"},
		{name: "static_header", clientType: authz.AuthClientTypeRemoteSession, header: "X-API-Key", value: "configured-key", required: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			fx := seedOrganizationGateway(t, ctx, ti)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			projectID := *ac.ProjectID
			server, err := mcpserversrepo.New(ti.conn).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{ProjectID: projectID, Slug: conv.ToPGText(fx.granted)})
			require.NoError(t, err)
			// Typed offers have no issuer binding or saved connection. Pending
			// legacy setup exercises both bound and unbound registrations.
			var bindings []uuid.UUID
			if tc.bound {
				bindings = []uuid.UUID{fx.issuerID}
			}
			registration := createConsentRemoteClient(t, ctx, ti.conn, projectID, ac.ActiveOrganizationID, "typed-registration", "", bindings)
			if !tc.noLegacyAssociation {
				require.NoError(t, testrepo.New(ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{ProjectID: projectID, OrganizationID: ac.ActiveOrganizationID, McpServerID: server.ID, RemoteSessionClientID: registration}))
			}
			if !tc.pending {
				clients := authclientsrepo.New(ti.conn)
				clientParams := authclientsrepo.CreateAuthClientParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: conv.ToNullUUID(projectID), Name: "Typed pending client", ClientType: tc.clientType, RemoteSessionClientID: conv.ToNullUUID(registration), ClientAttachmentScope: conv.ToPGText("project:" + projectID.String())}
				if tc.clientType == authz.AuthClientTypeStaticCredential {
					clientParams.RemoteSessionClientID = uuid.NullUUID{}
					clientParams.ClientAttachmentScope = conv.ToPGTextEmpty("")
				}
				client, err := clients.CreateAuthClient(ctx, clientParams)
				require.NoError(t, err)
				if tc.noLegacyAssociation {
					require.NoError(t, clients.CreateAuthClientHeader(ctx, authclientsrepo.CreateAuthClientHeaderParams{AuthClientID: client.ID, OrganizationID: ac.ActiveOrganizationID, Name: "X-API-Key", IsRequired: true, Value: conv.ToPGText("configured-client-key")}))
				}
				if tc.name == "required_caller_header" || tc.name == "admin_header_override" || tc.name == "detached_required_caller_header" {
					header := authclientsrepo.CreateAuthClientHeaderParams{AuthClientID: client.ID, OrganizationID: ac.ActiveOrganizationID, Name: "X-API-Key", IsRequired: true}
					if tc.name == "admin_header_override" {
						header.Value = conv.ToPGText("configured-client-key")
					} else {
						header.ValueFromRequestHeader = conv.ToPGText("X-API-Key")
					}
					require.NoError(t, clients.CreateAuthClientHeader(ctx, header))
				}
				_, err = clients.AttachAuthClientToServer(ctx, authclientsrepo.AttachAuthClientToServerParams{OrganizationID: ac.ActiveOrganizationID, McpServerID: server.ID, AuthClientID: client.ID})
				require.NoError(t, err)
				if tc.detached {
					_, err = clients.DetachAuthClientFromServer(ctx, authclientsrepo.DetachAuthClientFromServerParams{OrganizationID: ac.ActiveOrganizationID, McpServerID: server.ID, AuthClientID: client.ID})
					require.NoError(t, err)
					offers, err := upstreamclientsrepo.New(ti.conn).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: ac.ActiveOrganizationID, McpServerIds: []uuid.UUID{server.ID}})
					require.NoError(t, err)
					require.Len(t, offers, 1)
					require.False(t, offers[0].UpstreamAuthPending)
					require.False(t, offers[0].AuthClientID.Valid)
				}
			} else {
				offers, err := upstreamclientsrepo.New(ti.conn).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: ac.ActiveOrganizationID, McpServerIds: []uuid.UUID{server.ID}})
				require.NoError(t, err)
				require.Len(t, offers, 1)
				require.True(t, offers[0].UpstreamAuthPending)
				require.False(t, offers[0].AuthClientID.Valid)
			}
			if tc.header != "" {
				seedRemoteHeader(t, ctx, ti.conn, projectID, server.RemoteMcpServerID.UUID, tc.header, tc.value, tc.source, tc.required)
			}
			token := mintOrganizationGatewayBearer(t, ti, fx)
			endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, fx.organizationSlug, "mcp/gateway")
			require.NoError(t, err)
			stateID := uuid.NewString()
			require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{ID: stateID, UserSessionIssuerID: fx.issuerID, Endpoint: mcp.EndpointRef{McpSlug: endpoint.Slug, RouteBase: endpoint.RouteBase, MetaMcpServerID: endpoint.MetaMcpServerID}, RedirectURI: "http://example.com/cb", CSRFToken: "csrf-token", Subject: &fx.subject, CreatedAt: time.Now(), FirstParty: true, AutoConnectDone: true}))
			for _, mode := range []string{"off", "shadow", "enforced", "rollback"} {
				t.Run(mode, func(t *testing.T) { // Sequential transitions verify rollback of the same configuration.
					q := featuresrepo.New(ti.conn)
					params := featuresrepo.EnableFeatureParams{OrganizationID: ac.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)}
					_, err := q.EnableFeature(ctx, params)
					require.NoError(t, err)
					if mode != "enforced" {
						_, err = q.DeleteFeature(ctx, featuresrepo.DeleteFeatureParams(params))
						require.NoError(t, err)
					}
					mcp.ResetAuthEnforcementForTest(ti.service.Service)
					ti.features.SetFlag(feature.FlagAuthClientResolver, ac.ActiveOrganizationID, false)
					ti.features.SetFlag(feature.FlagAuthClientResolverShadow, ac.ActiveOrganizationID, mode == "shadow")
					visible := (tc.bound && tc.name != "detached_required_caller_header") || tc.name == "static_header" || (!tc.pending && !tc.detached && mode == "enforced" && tc.name != "required_caller_header")
					w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, token, listServersBody(t))
					require.NoError(t, err)
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					if visible {
						require.Contains(t, w.Body.String(), `"slug":"`+fx.granted+`"`)
					} else {
						require.NotContains(t, w.Body.String(), `"slug":"`+fx.granted+`"`)
					}
					require.NotContains(t, w.Body.String(), `"slug":"`+fx.denied+`"`)
					// Project and organization gateways share typed admission; neither
					// may require a legacy issuer link or widen the caller's access.
					projectToken := mintProjectGatewayBearer(t, ti, fx, fx.projectSlug)
					projectPage, err := serveProjectGatewayHTTP(t, ti, fx, fx.projectSlug, projectToken, listServersBody(t))
					require.NoError(t, err)
					require.Equal(t, http.StatusOK, projectPage.Code, projectPage.Body.String())
					if visible {
						require.Contains(t, projectPage.Body.String(), `"slug":"`+fx.granted+`"`)
					} else {
						require.NotContains(t, projectPage.Body.String(), `"slug":"`+fx.granted+`"`)
					}
					require.NotContains(t, projectPage.Body.String(), `"slug":"`+fx.denied+`"`)
					request := httptest.NewRequest(http.MethodGet, "/mcp/gateway/"+fx.organizationSlug+"/connect?state="+stateID+"&server="+url.QueryEscape(fx.granted), nil)
					page := httptest.NewRecorder()
					require.NoError(t, ti.service.ServeConsent(page, request, endpoint))
					require.Equal(t, http.StatusOK, page.Code)
					if visible {
						require.NotContains(t, page.Body.String(), "data-gateway-focus-unavailable")
					} else {
						require.Contains(t, page.Body.String(), "data-gateway-focus-unavailable")
					}
				})
			}
		})
	}
}
