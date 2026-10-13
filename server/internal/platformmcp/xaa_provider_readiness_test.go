package platformmcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestXAAReadinessSpeakeasyVendorWithoutOktaConnection(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_xaa_speakeasy_readiness")
	require.NoError(t, err)
	f := seedXAAInspectorFixture(t, ctx, conn, pgtype.Text{})
	fixtures := testrepo.New(conn)
	require.NoError(t, fixtures.SetPreparationFixtureIssuerCapability(ctx, testrepo.SetPreparationFixtureIssuerCapabilityParams{ID: f.issuerA, ProjectID: conv.ToNullUUID(f.projectID)}))
	bindXAAInspectorUpstream(t, ctx, conn, f, f.issuerA)
	require.NoError(t, fixtures.SeedInternalSessionIssuerSignInMethodsFixture(ctx, testrepo.SeedInternalSessionIssuerSignInMethodsFixtureParams{OrganizationID: f.organizationID, Issuer: "https://gram.example.test/oauth/" + uuid.NewString(), Name: conv.ToPGText("xaa-" + uuid.NewString()[:8])}))
	internal, err := jwksrepo.New(conn).GetOrganizationInternalSessionIssuer(ctx, f.organizationID)
	require.NoError(t, err)
	require.NoError(t, remotesessionsrepo.New(conn).InsertEMAProvider(ctx, remotesessionsrepo.InsertEMAProviderParams{OrganizationID: f.organizationID, VendorIssuer: "https://as-a.example.test", InternalSessionIssuerID: uuid.NullUUID{UUID: internal.ID, Valid: true}}))

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagEMAProviders, f.organizationID, true)
	speakeasy := remotesessions.EMAProvider{Kind: remotesessions.EMAProviderSpeakeasy, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: internal.ID, Unavailable: ""}
	governor := selectedGovernor{recordingGovernor: &recordingGovernor{issuer: f.issuerA, serves: true}, selection: remotesessions.EMAProviderSelection{VendorIssuer: "https://as-a.example.test", IsDefault: false, Provider: speakeasy}, selected: true}
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	// No Okta connections reader: the organization has no Okta connection.
	service := NewPostgresReader(testenv.NewLogger(t), conn).WithXAAReadiness(nil, nil, flags).WithXAAIdentityChaining(governor, nil).WithAuthorization(engine).xaaReadiness
	require.NotNil(t, service)

	user := "user-" + uuid.NewString()
	actor := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.organizationID, UserID: user}, urn.NewPrincipal(urn.PrincipalTypeUser, user))
	actor = contextvalues.SetActingSurface(actor, contextvalues.ActingSurfacePlatformMCP)
	admin := authz.NewGrant(authz.ScopeOrgAdmin, f.organizationID)

	server, ok, err := service.servers.Server(authz.GrantsToContext(actor, []authz.Grant{admin, authz.NewGrant(authz.ScopeMCPRead, f.serverID.String())}), f.organizationID, f.projectID, f.serverID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, remotesessions.EMAProviderSpeakeasy, server.provider, "the vendor's selection is read through the provider resolver")

	result, output, err := service.read(authz.GrantsToContext(actor, []authz.Grant{admin, authz.NewGrant(authz.ScopeMCPRead, f.serverID.String())}), f.organizationID, f.projectID, f.serverID)
	require.NoError(t, err)
	require.Nil(t, result, "a Speakeasy-selected vendor reports readiness with no Okta connection")
	require.Equal(t, xaaStateReady, output.State)
	require.False(t, output.Pending)
	require.Nil(t, output.OktaSignIn)
	require.NotNil(t, output.IdentityChaining)
	require.True(t, output.IdentityChaining.Served)
	require.Equal(t, string(remotesessions.EMAProviderSpeakeasy), output.IdentityChaining.Provider.Kind)
	require.Len(t, output.IdentityChaining.Bindings, 1)

	result, _, err = service.read(authz.GrantsToContext(actor, []authz.Grant{admin}), f.organizationID, f.projectID, f.serverID)
	require.NoError(t, err)
	requireXAARefusal(t, result, "not_found")

	result, _, err = service.read(authz.GrantsToContext(actor, []authz.Grant{admin, authz.NewGrant(authz.ScopeMCPRead, f.serverID.String())}), f.organizationID, uuid.New(), f.serverID)
	require.NoError(t, err)
	requireXAARefusal(t, result, "not_found")

	flags.SetFlag(feature.FlagEMAProviders, f.organizationID, false)
	result, _, err = service.read(authz.GrantsToContext(actor, []authz.Grant{admin, authz.NewGrant(authz.ScopeMCPRead, f.serverID.String())}), f.organizationID, f.projectID, f.serverID)
	require.NoError(t, err)
	requireXAARefusal(t, result, unavailableCode)
}

func requireXAARefusal(t *testing.T, result *mcp.CallToolResult, code string) {
	t.Helper()
	require.NotNil(t, result)
	require.True(t, result.IsError)
	content, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, content.Text, `"code":"`+code+`"`)
}
