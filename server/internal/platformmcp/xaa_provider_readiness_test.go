package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	flags.SetFlag(feature.FlagOktaConnections, f.organizationID, true)
	speakeasy := remotesessions.EMAProvider{Kind: remotesessions.EMAProviderSpeakeasy, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: internal.ID, Unavailable: ""}
	governor := orderedGovernor{recordingGovernor: &recordingGovernor{issuer: f.issuerA, serves: true}, order: remotesessions.EMAProviderOrder{VendorIssuer: "https://as-a.example.test", IsDefault: false, Providers: []remotesessions.EMAProvider{speakeasy}}, ordered: true}
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	// The Okta rollout is on but the organization has no Okta connection.
	okta := &xaaReaderStub{err: oops.C(oops.CodeFailedPrecondition)}
	service := NewPostgresReader(testenv.NewLogger(t), conn).WithXAAReadiness(okta, nil, flags).WithXAAIdentityChaining(governor, nil).WithAuthorization(engine).xaaReadiness
	require.NotNil(t, service)

	user := "user-" + uuid.NewString()
	actor := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.organizationID, UserID: user}, urn.NewPrincipal(urn.PrincipalTypeUser, user))
	actor = contextvalues.SetActingSurface(actor, contextvalues.ActingSurfacePlatformMCP)
	admin := authz.NewGrant(authz.ScopeOrgAdmin, f.organizationID)

	server, ok, err := service.servers.Server(authz.GrantsToContext(actor, []authz.Grant{admin, authz.NewGrant(authz.ScopeMCPRead, f.serverID.String())}), f.organizationID, f.projectID, f.serverID)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, server.speakeasy, "the vendor's order is read through the provider resolver")

	result, output, err := service.read(authz.GrantsToContext(actor, []authz.Grant{admin, authz.NewGrant(authz.ScopeMCPRead, f.serverID.String())}), f.organizationID, f.projectID, f.serverID)
	require.NoError(t, err)
	require.Nil(t, result, "a Speakeasy-selected vendor reports readiness with no Okta connection")
	require.Equal(t, xaaStateReady, output.State)
	require.False(t, output.Pending)
	require.Nil(t, output.OktaSignIn)
	require.NotNil(t, output.IdentityChaining)
	require.True(t, output.IdentityChaining.Served)
	require.Equal(t, string(remotesessions.EMAProviderSpeakeasy), output.IdentityChaining.Providers[0].Kind)
	require.Nil(t, output.IdentityChaining.Providers[0].LastAcceptedAt, "no credential has been accepted yet")
	require.Len(t, output.IdentityChaining.Bindings, 1)
	require.Equal(t, 1, okta.calls, "the Okta reader reported no connection before the fallback")

	binding, err := remotesessionsrepo.New(conn).GetEMABinding(ctx, remotesessionsrepo.GetEMABindingParams{ProjectID: f.projectID, OrganizationID: f.organizationID, UserSessionIssuerID: f.userIssuer, RemoteSessionIssuerID: f.issuerA, Resource: "https://upstream.example.test/mcp"})
	require.NoError(t, err)
	accepted := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	_, err = remotesessionsrepo.New(conn).PlantEMACredentialFixture(ctx, remotesessionsrepo.PlantEMACredentialFixtureParams{
		OrganizationID: conv.ToPGText(f.organizationID), ProjectID: conv.ToNullUUID(f.projectID), UserSessionIssuerID: conv.ToNullUUID(f.userIssuer),
		RemoteSessionIssuerID: conv.ToNullUUID(f.issuerA), RemoteSessionClientID: binding.RemoteSessionClientID, Resource: "https://upstream.example.test/mcp",
		SubjectUrn: urn.NewUserSubject(user).String(), EmaProvider: string(remotesessions.EMAProviderSpeakeasy), Retired: false, UpdatedAt: conv.ToPGTimestamptz(accepted),
		InternalSessionIssuerID: conv.ToNullUUID(internal.ID), TrustedIssuerSessionID: uuid.NullUUID{}, BindingGeneration: pgtype.Int8{},
	})
	require.NoError(t, err)
	_, output, err = service.read(authz.GrantsToContext(actor, []authz.Grant{admin, authz.NewGrant(authz.ScopeMCPRead, f.serverID.String())}), f.organizationID, f.projectID, f.serverID)
	require.NoError(t, err)
	require.NotNil(t, output.IdentityChaining)
	require.NotNil(t, output.IdentityChaining.Providers[0].LastAcceptedAt)
	require.Equal(t, accepted.Format(time.RFC3339), *output.IdentityChaining.Providers[0].LastAcceptedAt, "the readiness view reports when the vendor last accepted a Speakeasy assertion")

	result, _, err = service.read(authz.GrantsToContext(actor, []authz.Grant{admin}), f.organizationID, f.projectID, f.serverID)
	require.NoError(t, err)
	requireXAARefusal(t, result, "not_found")

	result, _, err = service.read(authz.GrantsToContext(actor, []authz.Grant{admin, authz.NewGrant(authz.ScopeMCPRead, f.serverID.String())}), f.organizationID, uuid.New(), f.serverID)
	require.NoError(t, err)
	requireXAARefusal(t, result, "not_found")

	flags.SetFlag(feature.FlagEMAProviders, f.organizationID, false)
	result, _, err = service.read(authz.GrantsToContext(actor, []authz.Grant{admin, authz.NewGrant(authz.ScopeMCPRead, f.serverID.String())}), f.organizationID, f.projectID, f.serverID)
	require.NoError(t, err)
	requireXAARefusal(t, result, "setup_required")

	flags.SetFlag(feature.FlagOktaConnections, f.organizationID, false)
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

func TestSpeakeasyReadinessViewFollowsSignIn(t *testing.T) {
	t.Parallel()
	speakeasy := remotesessions.EMAProvider{Kind: remotesessions.EMAProviderSpeakeasy, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: uuid.New(), Unavailable: "", LastAcceptedAt: nil}
	external := remotesessions.EMAProvider{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: uuid.New(), InternalSessionIssuerID: uuid.Nil, Unavailable: "", LastAcceptedAt: nil}
	order := func(providers ...remotesessions.EMAProvider) remotesessions.EMAProviderOrder {
		return remotesessions.EMAProviderOrder{VendorIssuer: "https://vendor.example.test", IsDefault: false, Providers: providers}
	}
	require.True(t, speakeasyReadinessView(order(speakeasy), false))
	require.True(t, speakeasyReadinessView(order(speakeasy, external), false), "a Speakeasy sign-in is vouched for by Speakeasy")
	require.False(t, speakeasyReadinessView(order(speakeasy, external), true), "a federated sign-in is vouched for by its external issuer")
	require.True(t, speakeasyReadinessView(order(speakeasy), true), "with no external issuer named there is no connection to describe")
	require.False(t, speakeasyReadinessView(order(external), false))

	disabled := speakeasy
	disabled.Unavailable = remotesessions.EMAProviderDisabled
	require.True(t, speakeasyReadinessView(order(disabled), false), "a disabled Speakeasy issuer keeps the identity-chaining view")
	require.True(t, speakeasyReadinessView(order(disabled, external), false))
	require.True(t, speakeasyReadinessView(order(disabled), true))
	require.False(t, speakeasyReadinessView(order(disabled, external), true))
}
