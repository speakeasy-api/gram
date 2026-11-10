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
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServeOrganizationGateway_HostedAuthenticationEligibility(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		// oauth tools also accept a token from the member's upstream sign-in.
		oauth bool
		// client attaches a sign-in client to the gateway's issuer.
		client bool
		// configured stores the tools' API key in the member environment.
		configured bool
		// private turns off the toolset's public MCP access; the caller holds
		// no toolset-level grant.
		private bool
		listed  bool
	}{
		{name: "unsupported", oauth: false, client: false, configured: false, private: false, listed: false},
		{name: "configured", oauth: false, client: false, configured: true, private: false, listed: true},
		{name: "oauth", oauth: true, client: true, configured: false, private: false, listed: true},
		{name: "oauth without client", oauth: true, client: false, configured: false, private: false, listed: false},
		{name: "configured with sign-in but no client", oauth: true, client: false, configured: true, private: false, listed: true},
		{name: "private toolset", oauth: false, client: false, configured: true, private: true, listed: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			fx := seedOrganizationGateway(t, ctx, ti)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			slug := "auth-member-" + uuid.NewString()[:8]
			toolset := createPublicMCPToolset(t, ctx, ti, auth, slug)
			server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{ID: uuid.New(), ProjectID: *auth.ProjectID, Name: conv.ToPGText(tt.name), Slug: conv.ToPGText(slug), ToolsetID: conv.ToNullUUID(toolset.ID), Visibility: mcpservers.VisibilityPublic})
			require.NoError(t, err)
			if tt.oauth {
				ti.addToolWithDualSecurity(ctx, t, toolset.ID, *auth.ProjectID, auth.ActiveOrganizationID)
				issuer, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{ProjectID: conv.ToNullUUID(*auth.ProjectID), Slug: "gateway-oauth", Issuer: "https://oauth.example.com", AuthorizationEndpoint: conv.ToPGText("https://oauth.example.com/authorize"), TokenEndpoint: conv.ToPGText("https://oauth.example.com/token"), ScopesSupported: []string{}, GrantTypesSupported: []string{"authorization_code"}, ResponseTypesSupported: []string{"code"}, TokenEndpointAuthMethodsSupported: []string{"none"}, CodeChallengeMethodsSupported: []string{"S256"}})
				require.NoError(t, err)
				stampRemoteSessionIssuer(t, ctx, ti.conn, *auth.ProjectID, server.ID, conv.ToNullUUID(issuer.ID))
				if tt.client {
					client, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{ProjectID: conv.ToNullUUID(*auth.ProjectID), OrganizationID: conv.ToPGTextEmpty(auth.ActiveOrganizationID), RemoteSessionIssuerID: issuer.ID, ClientID: "gateway-oauth-client"})
					require.NoError(t, err)
					require.NoError(t, remotesessionsrepo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: client.ID, UserSessionIssuerID: fx.issuerID}))
				}
			} else {
				ti.addToolWithSecurity(ctx, t, toolset.ID, *auth.ProjectID, auth.ActiveOrganizationID)
			}
			if tt.configured {
				env, err := environmentsrepo.New(ti.conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{OrganizationID: auth.ActiveOrganizationID, ProjectID: *auth.ProjectID, Name: "Gateway fixture", Slug: "gateway-fixture"})
				require.NoError(t, err)
				_, err = environmentsrepo.New(ti.conn).UpsertEnvironmentEntry(ctx, environmentsrepo.UpsertEnvironmentEntryParams{Name: "TEST_API_KEY", Value: "fixture-credential", EnvironmentID: env.ID, ProjectID: *auth.ProjectID})
				require.NoError(t, err)
				_, err = mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{ID: server.ID, Name: server.Name, Slug: server.Slug, ToolsetID: conv.ToNullUUID(toolset.ID), ProjectID: *auth.ProjectID, EnvironmentID: uuid.NullUUID{UUID: env.ID, Valid: true}, Visibility: mcpservers.VisibilityPublic})
				require.NoError(t, err)
			}
			// The toolset-level connect check follows the canonical server's visibility.
			testenv.SetHostedMCP(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, testenv.HostedMCP{Visibility: conv.Ternary(tt.private, "private", "public")})
			w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listServersBody(t))
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			if tt.listed {
				require.Contains(t, w.Body.String(), slug)
			} else {
				require.NotContains(t, w.Body.String(), slug)
			}
		})
	}
}

// Toolsets with Platform tools are never distributed automatically, so the
// organization gateway leaves them out just as automatic plugin delivery does.
func TestServeOrganizationGateway_ExcludesPlatformToolsets(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		tools  []urn.Tool
		listed bool
	}{
		{name: "without tools", tools: []urn.Tool{}, listed: true},
		{name: "with a platform tool", tools: []urn.Tool{urn.NewTool(urn.ToolKindPlatform, "slack", "add_reaction")}, listed: false},
		// Only the kind segment marks a Platform tool.
		{name: "with a platform-named source", tools: []urn.Tool{urn.NewTool(urn.ToolKindHTTP, "platform", "add_reaction")}, listed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			fx := seedOrganizationGateway(t, ctx, ti)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			slug := "platform-member-" + uuid.NewString()[:8]
			toolset := createPublicMCPToolset(t, ctx, ti, auth, slug)
			_, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{ID: uuid.New(), ProjectID: *auth.ProjectID, Name: conv.ToPGText(tt.name), Slug: conv.ToPGText(slug), ToolsetID: conv.ToNullUUID(toolset.ID), Visibility: mcpservers.VisibilityPublic})
			require.NoError(t, err)
			_, err = toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{ToolsetID: toolset.ID, Version: 1, ToolUrns: tt.tools, ResourceUrns: []urn.Resource{}, PredecessorID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
			require.NoError(t, err)

			w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, mintOrganizationGatewayBearer(t, ti, fx), listServersBody(t))
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			if tt.listed {
				require.Contains(t, w.Body.String(), slug)
			} else {
				require.NotContains(t, w.Body.String(), slug)
			}
		})
	}
}
