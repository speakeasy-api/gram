package mcp_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServeOrganizationGateway_HostedAuthenticationEligibility(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"unsupported", "configured", "oauth", "oauth-no-client"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			fx := seedOrganizationGateway(t, ctx, ti)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			slug := "auth-member-" + uuid.NewString()[:8]
			toolset := createPublicMCPToolset(t, ctx, toolsetsrepo.New(ti.conn), auth, slug)
			server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{ID: uuid.New(), ProjectID: *auth.ProjectID, Name: conv.ToPGText(mode), Slug: conv.ToPGText(slug), ToolsetID: conv.ToNullUUID(toolset.ID), Visibility: mcpservers.VisibilityPublic})
			require.NoError(t, err)
			if mode == "oauth" || mode == "oauth-no-client" {
				ti.addToolWithDualSecurity(ctx, t, toolset.ID, *auth.ProjectID, auth.ActiveOrganizationID)
				issuer, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{ProjectID: conv.ToNullUUID(*auth.ProjectID), Slug: "gateway-oauth", Issuer: "https://oauth.example.com", AuthorizationEndpoint: conv.ToPGText("https://oauth.example.com/authorize"), TokenEndpoint: conv.ToPGText("https://oauth.example.com/token"), ScopesSupported: []string{}, GrantTypesSupported: []string{"authorization_code"}, ResponseTypesSupported: []string{"code"}, TokenEndpointAuthMethodsSupported: []string{"none"}, CodeChallengeMethodsSupported: []string{"S256"}})
				require.NoError(t, err)
				stampRemoteSessionIssuer(t, ctx, ti.conn, *auth.ProjectID, server.ID, conv.ToNullUUID(issuer.ID))
				if mode == "oauth" {
					// Only a client on the gateway's issuer lets a person connect the member.
					client, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGTextEmpty(auth.ActiveOrganizationID), RemoteSessionIssuerID: issuer.ID, ClientID: "gateway-oauth-client"})
					require.NoError(t, err)
					require.NoError(t, remotesessionsrepo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: client.ID, UserSessionIssuerID: fx.issuerID}))
				}
			} else {
				key := ti.addToolWithSecurity(ctx, t, toolset.ID, *auth.ProjectID, auth.ActiveOrganizationID)
				if mode == "configured" {
					env, err := environmentsrepo.New(ti.conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, Name: "Gateway fixture", Slug: "gateway-fixture"})
					require.NoError(t, err)
					_, err = environmentsrepo.New(ti.conn).UpsertEnvironmentEntry(ctx, environmentsrepo.UpsertEnvironmentEntryParams{Name: key, Value: "fixture-credential", EnvironmentID: env.ID, ProjectID: *auth.ProjectID})
					require.NoError(t, err)
					_, err = mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{ID: server.ID, Name: server.Name, Slug: server.Slug, ToolsetID: conv.ToNullUUID(toolset.ID), ProjectID: *auth.ProjectID, EnvironmentID: uuid.NullUUID{UUID: env.ID, Valid: true}, Visibility: mcpservers.VisibilityPublic})
					require.NoError(t, err)
				}
			}
			// Independent wrappers do not inherit legacy endpoint enablement.
			setToolsetMcpDisabled(t, ctx, ti, toolset.ID, *auth.ProjectID)
			w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listServersBody(t))
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			if mode == "unsupported" || mode == "oauth-no-client" {
				require.NotContains(t, w.Body.String(), slug)
			} else {
				require.Contains(t, w.Body.String(), slug)
			}
		})
	}
}
