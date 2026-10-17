package platformmcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	srv "github.com/speakeasy-api/gram/server/gen/okta_resource_connections"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type xaaInspectorFixture struct {
	organizationID string
	projectID      uuid.UUID
	serverID       uuid.UUID
	issuerA        uuid.UUID
	issuerB        uuid.UUID
	trustedClient  uuid.UUID
	userIssuer     uuid.UUID
}

// seedXAAInspectorFixture builds a direct remote server stamped with issuer A,
// a second issuer B in the project, and an organization sign-in issuer whose
// trusted client records callbackBase.
func seedXAAInspectorFixture(t *testing.T, ctx context.Context, conn *pgxpool.Pool, callbackBase pgtype.Text) xaaInspectorFixture {
	t.Helper()
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	org := principal.OrganizationID
	rs := remotesessionsrepo.New(conn)
	newIssuer := func(slug, issuer string, projectScoped bool) uuid.UUID {
		params := remotesessionsrepo.CreateRemoteSessionIssuerParams{
			OrganizationID: conv.ToPGText(org), Slug: slug + "-" + uuid.NewString()[:8], Issuer: issuer,
			ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{},
			TokenEndpointAuthMethodsSupported: []string{}, CodeChallengeMethodsSupported: []string{},
		}
		if projectScoped {
			params.ProjectID = conv.ToNullUUID(project.ID)
		}
		row, err := rs.CreateRemoteSessionIssuer(ctx, params)
		require.NoError(t, err)
		return row.ID
	}
	idpIssuer := newIssuer("idp", "https://idp.example.test", false)
	trusted, err := rs.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		OrganizationID: conv.ToPGText(org), RemoteSessionIssuerID: idpIssuer, ClientID: "idp-client",
		ClientIDIssuedAt: conv.ToPGTimestamptz(time.Now().UTC()), TokenEndpointAuthMethod: conv.ToPGText("none"), Scope: []string{}, CallbackBaseUrl: callbackBase,
	})
	require.NoError(t, err)
	usi, err := usersessionsrepo.New(conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID: conv.ToPGText(org), Slug: "xaa-usi-" + uuid.NewString()[:8], AuthnChallengeMode: "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: time.Hour.Microseconds(), Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: idpIssuer, Valid: true}, TrustedRemoteSessionClientID: uuid.NullUUID{UUID: trusted.ID, Valid: true},
	})
	require.NoError(t, err)
	issuerA := newIssuer("as-a", "https://as-a.example.test", true)
	issuerB := newIssuer("as-b", "https://as-b.example.test", true)

	remote, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{ID: uuid.New(), ProjectID: project.ID, TransportType: "streamable-http", Url: "https://upstream.example.test/mcp"})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: project.ID, Name: conv.ToPGText("XAA inspector"), Slug: conv.ToPGText("xaa-inspector-" + uuid.NewString()[:8]),
		UserSessionIssuerID: uuid.NullUUID{UUID: usi.ID, Valid: true}, RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	stamped, err := testrepo.New(conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		RemoteSessionIssuerID: conv.ToNullUUID(issuerA), ID: server.ID, ProjectID: project.ID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), stamped)
	return xaaInspectorFixture{organizationID: org, projectID: project.ID, serverID: server.ID, issuerA: issuerA, issuerB: issuerB, trustedClient: trusted.ID, userIssuer: usi.ID}
}

// bindXAAInspectorUpstream records a ready binding of the fixture's upstream
// under issuer.
func bindXAAInspectorUpstream(t *testing.T, ctx context.Context, conn *pgxpool.Pool, f xaaInspectorFixture, issuer uuid.UUID) {
	t.Helper()
	bindXAAInspectorResource(t, ctx, conn, f, issuer, "https://upstream.example.test/mcp")
}

func bindXAAInspectorResource(t *testing.T, ctx context.Context, conn *pgxpool.Pool, f xaaInspectorFixture, issuer uuid.UUID, resource string) {
	t.Helper()
	q := remotesessionsrepo.New(conn)
	client, err := q.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		OrganizationID: conv.ToPGText(f.organizationID), ProjectID: conv.ToNullUUID(f.projectID), RemoteSessionIssuerID: issuer, ClientID: "resource-" + uuid.NewString()[:8],
		ClientIDIssuedAt: conv.ToPGTimestamptz(time.Now().UTC()), TokenEndpointAuthMethod: conv.ToPGText("none"), Scope: []string{},
	})
	require.NoError(t, err)
	key := remotesessionsrepo.GetEMABindingParams{ProjectID: f.projectID, OrganizationID: f.organizationID, UserSessionIssuerID: f.userIssuer, RemoteSessionIssuerID: issuer, Resource: resource}
	require.NoError(t, q.EnsureEMABinding(ctx, remotesessionsrepo.EnsureEMABindingParams(key)))
	current, err := q.GetEMABinding(ctx, key)
	require.NoError(t, err)
	_, err = q.SetEMABinding(ctx, remotesessionsrepo.SetEMABindingParams{
		State: conv.ToPGText(remotesessions.PreparationStateReady), RemoteSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true},
		Generation: current.Generation + 1, GrantSource: conv.ToPGText(remotesessions.PreparationGrantSourceAdministratorDeclared),
		RequestedScopes: []string{"read"}, ID: current.ID, ProjectID: f.projectID, OrganizationID: f.organizationID, ExpectedGeneration: current.Generation,
	})
	require.NoError(t, err)
}

func TestXAAIdentityInspectorServedSelectsDirectUpstreamAcrossIssuers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_xaa_inspector_served")
	require.NoError(t, err)
	f := seedXAAInspectorFixture(t, ctx, conn, pgtype.Text{})
	bindXAAInspectorUpstream(t, ctx, conn, f, f.issuerB)

	governor := &recordingGovernor{issuer: f.issuerB, serves: true}
	inspector := &postgresXAAIdentityInspector{db: conn, governor: governor, origins: remotesessions.CallbackOrigins{Outbound: nil, Registration: nil}}
	chaining, callback, err := inspector.Inspect(ctx, f.organizationID, f.projectID, f.serverID, f.issuerA, "https://upstream.example.test/mcp")
	require.NoError(t, err)
	require.True(t, chaining.Served, "a direct upstream served through another issuer's binding is served at runtime")
	require.Nil(t, callback, "no recorded or pinned origin yields no callback")
	require.Len(t, governor.recorded(), 1)
	req := governor.recorded()[0]
	require.False(t, req.RemoteSessionIssuerID.Valid, "a direct upstream selects by resource across issuers, as the runtime does")
	require.Equal(t, "https://upstream.example.test/mcp", req.UpstreamResource)
	require.Len(t, chaining.Bindings, 1, "the binding that serves the upstream is reported even under another issuer")

	bindXAAInspectorUpstream(t, ctx, conn, f, f.issuerA)
	chaining, _, err = inspector.Inspect(ctx, f.organizationID, f.projectID, f.serverID, f.issuerA, "https://upstream.example.test/mcp")
	require.NoError(t, err)
	require.Len(t, chaining.Bindings, 2, "both issuers' bindings are reported so the ambiguity is visible")
}

func TestXAAIdentityInspectorCallbackUsesRecordedOrigin(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_xaa_inspector_callback")
	require.NoError(t, err)
	f := seedXAAInspectorFixture(t, ctx, conn, conv.ToPGText("https://recorded.example.test"))

	inspector := &postgresXAAIdentityInspector{db: conn, governor: nil, origins: remotesessions.CallbackOrigins{Outbound: nil, Registration: nil}}
	_, callback, err := inspector.Inspect(ctx, f.organizationID, f.projectID, f.serverID, f.issuerA, "https://upstream.example.test/mcp")
	require.NoError(t, err)
	require.NotNil(t, callback, "a recorded callback origin needs no pinned outbound origin")
	require.Equal(t, "https://recorded.example.test"+remotesessions.FederatedIDPCallbackPath(f.trustedClient), *callback)
}

// orderedGovernor reports a fixed EMA provider order.
type orderedGovernor struct {
	*recordingGovernor
	order   remotesessions.EMAProviderOrder
	ordered bool
	err     error
}

func (g orderedGovernor) ProviderOrder(context.Context, identitychaining.Request) (remotesessions.EMAProviderOrder, bool, error) {
	return g.order, g.ordered, g.err
}

func TestXAAIdentityInspectorReportsTheProviderOrder(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_xaa_inspector_providers")
	require.NoError(t, err)
	f := seedXAAInspectorFixture(t, ctx, conn, pgtype.Text{})
	bindXAAInspectorUpstream(t, ctx, conn, f, f.issuerA)
	external := remotesessions.EMAProvider{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: f.issuerA, InternalSessionIssuerID: uuid.Nil, Unavailable: ""}
	untrusted := remotesessions.EMAProvider{Kind: remotesessions.EMAProviderExternal, RemoteSessionIssuerID: f.issuerB, InternalSessionIssuerID: uuid.Nil, Unavailable: remotesessions.EMAProviderNotTrustedByEndpoint}
	pending := remotesessions.EMAProvider{Kind: remotesessions.EMAProviderSpeakeasy, RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: uuid.New(), Unavailable: remotesessions.EMAProviderSigningUnavailable}
	order := func(providers ...remotesessions.EMAProvider) remotesessions.EMAProviderOrder {
		return remotesessions.EMAProviderOrder{VendorIssuer: "", IsDefault: false, Providers: providers}
	}
	inspect := func(g orderedGovernor) XAAIdentityChaining {
		inspector := &postgresXAAIdentityInspector{db: conn, governor: g, origins: remotesessions.CallbackOrigins{Outbound: nil, Registration: nil}}
		chaining, _, err := inspector.Inspect(ctx, f.organizationID, f.projectID, f.serverID, f.issuerA, "https://upstream.example.test/mcp")
		require.NoError(t, err)
		return chaining
	}

	served := inspect(orderedGovernor{recordingGovernor: &recordingGovernor{issuer: f.issuerA, serves: true}, order: order(pending, untrusted, external), ordered: true})
	require.True(t, served.Served, "a provider later in the order serves")
	require.Nil(t, served.NotServedReason)
	require.Equal(t, []XAAEMAProvider{
		{Kind: "speakeasy", UnavailableReason: new("signing_unavailable")},
		{Kind: "external", RemoteSessionIssuerID: new(f.issuerB.String()), UnavailableReason: new("not_trusted_by_endpoint")},
		{Kind: "external", RemoteSessionIssuerID: new(f.issuerA.String())},
	}, served.Providers, "every provider is listed in order with why it cannot serve")

	unavailable := inspect(orderedGovernor{recordingGovernor: &recordingGovernor{issuer: f.issuerA, serves: false}, order: order(pending, untrusted), ordered: true})
	require.False(t, unavailable.Served)
	require.Equal(t, "provider_unavailable", conv.PtrValOr(unavailable.NotServedReason, ""), "no provider in the order can serve")
	require.Len(t, unavailable.Providers, 2)

	unbound := inspect(orderedGovernor{recordingGovernor: &recordingGovernor{issuer: f.issuerA, serves: false}, order: remotesessions.EMAProviderOrder{}, ordered: false})
	require.Equal(t, "binding_not_ready", conv.PtrValOr(unbound.NotServedReason, ""))
	require.Nil(t, unbound.Providers)

	unreadable := inspect(orderedGovernor{recordingGovernor: &recordingGovernor{issuer: f.issuerA, serves: false}, order: remotesessions.EMAProviderOrder{}, ordered: false, err: identitychaining.ErrProviderOrderUnavailable})
	require.False(t, unreadable.Served)
	require.Equal(t, "provider_order_unavailable", conv.PtrValOr(unreadable.NotServedReason, ""), "an unreadable order is not reported as a missing binding")
	require.Nil(t, unreadable.Providers)
}

type failedResourceGovernor struct {
	*recordingGovernor
	err   error
	calls int
}

func (g *failedResourceGovernor) ResolveResource(context.Context, identitychaining.Request) (protectedresource.EndpointResource, error) {
	g.calls++
	return protectedresource.EndpointResource{}, g.err
}
func (*failedResourceGovernor) ProviderOrder(context.Context, identitychaining.Request) (remotesessions.EMAProviderOrder, bool, error) {
	panic("failed discovery must not be probed a second time")
}

func TestXAAIdentityInspectorPreservesSnapshotAfterDiscoveryFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"invalid metadata", protectedresource.ErrEndpointResourceInvalid},
		{"transient metadata failure", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			conn, err := platformMCPInfra.CloneTestDatabase(t, "xaa_failed_resource")
			require.NoError(t, err)
			f := seedXAAInspectorFixture(t, ctx, conn, conv.ToPGText("https://recorded.example.test"))
			bindXAAInspectorResource(t, ctx, conn, f, f.issuerA, "https://upstream.example.test")
			governor := &failedResourceGovernor{recordingGovernor: &recordingGovernor{}, err: tc.err}
			inspector := &postgresXAAIdentityInspector{logger: testenv.NewLogger(t), db: conn, governor: governor, origins: remotesessions.CallbackOrigins{}}
			result, callback, err := inspector.Inspect(ctx, f.organizationID, f.projectID, f.serverID, f.issuerA, "https://upstream.example.test/mcp")
			require.NoError(t, err)
			require.False(t, result.Served)
			require.Equal(t, "resource_unavailable", conv.PtrValOr(result.NotServedReason, ""))
			require.Len(t, result.Bindings, 1, "stored diagnostics survive failed metadata resolution")
			require.NotNil(t, callback)
			require.Equal(t, 1, governor.calls)
		})
	}
}

func TestXAAIdentityInspectorExplainsANonRoutableServer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_xaa_inspector_not_routable")
	require.NoError(t, err)
	f := seedXAAInspectorFixture(t, ctx, conn, pgtype.Text{})

	inspector := &postgresXAAIdentityInspector{db: conn, governor: &recordingGovernor{issuer: f.issuerA, serves: true}, origins: remotesessions.CallbackOrigins{Outbound: nil, Registration: nil}}
	for name, call := range map[string]struct {
		issuer   uuid.UUID
		resource string
	}{
		"no upstream issuer": {issuer: uuid.Nil, resource: "https://upstream.example.test/mcp"},
		"no resource":        {issuer: f.issuerA, resource: ""},
	} {
		chaining, _, err := inspector.Inspect(ctx, f.organizationID, f.projectID, f.serverID, call.issuer, call.resource)
		require.NoError(t, err, name)
		require.False(t, chaining.Served, name)
		require.NotNil(t, chaining.NotServedReason, name)
		require.Equal(t, xaaNotRoutable, *chaining.NotServedReason, name)
	}
}

// fixtureOrgInspector reads the fixture's organization; the test session's
// principal belongs to a placeholder one.
type fixtureOrgInspector struct {
	inner          *postgresXAAIdentityInspector
	organizationID string
}

func (f fixtureOrgInspector) Inspect(ctx context.Context, _ string, projectID, serverID, issuerID uuid.UUID, resource string) (XAAIdentityChaining, *string, error) {
	return f.inner.Inspect(ctx, f.organizationID, projectID, serverID, issuerID, resource)
}

func TestXAAReadinessToolReportsConnectionScopes(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_xaa_connection_scopes")
	require.NoError(t, err)
	f := seedXAAInspectorFixture(t, ctx, conn, pgtype.Text{})
	const upstream = "https://upstream.example.test/mcp"
	bindXAAInspectorUpstream(t, ctx, conn, f, f.issuerA)

	issuerID := f.issuerA.String()
	reader := &xaaReaderStub{result: &srv.ListOktaResourceConnectionsResult{Servers: []*srv.OktaResourceConnectionServer{
		{ProjectID: f.projectID.String(), McpServerID: f.serverID.String(), State: "connected", IssuerID: &issuerID, ResourceIndicator: upstream},
	}}}
	inspector := &postgresXAAIdentityInspector{db: conn, governor: nil, origins: remotesessions.CallbackOrigins{Outbound: nil, Registration: nil}}
	session := xaaTestSession(t, &xaaReadinessService{connections: reader, enabled: func(context.Context, string) (bool, error) { return true, nil }, identity: fixtureOrgInspector{inner: inspector, organizationID: f.organizationID}}, false)
	call := func() (XAAIdentityChainingBinding, map[string]any) {
		t.Helper()
		result := xaaCall(t, session, f.projectID.String(), f.serverID.String())
		if result.IsError {
			text, _ := result.Content[0].(*mcp.TextContent)
			require.FailNow(t, "tool error", "%+v", text)
		}
		wire, err := json.Marshal(result.StructuredContent)
		require.NoError(t, err)
		var output GetXAAReadinessOutput
		require.NoError(t, json.Unmarshal(wire, &output))
		require.NotNil(t, output.IdentityChaining)
		require.Len(t, output.IdentityChaining.Bindings, 1)
		var raw struct {
			IdentityChaining struct {
				Bindings []map[string]any `json:"bindings"`
			} `json:"identity_chaining"`
		}
		require.NoError(t, json.Unmarshal(wire, &raw))
		return output.IdentityChaining.Bindings[0], raw.IdentityChaining.Bindings[0]
	}

	binding, raw := call()
	require.Equal(t, []string{"read"}, binding.ConnectionScopes, "an unlinked client has no connection to follow, so it reports the recorded scopes")
	require.False(t, binding.ScopesOutOfDate)
	require.Contains(t, raw, "scopes_out_of_date")

	q := remotesessionsrepo.New(conn)
	current, err := q.GetEMABinding(ctx, remotesessionsrepo.GetEMABindingParams{ProjectID: f.projectID, OrganizationID: f.organizationID, UserSessionIssuerID: f.userIssuer, RemoteSessionIssuerID: f.issuerA, Resource: upstream})
	require.NoError(t, err)
	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: current.RemoteSessionClientID.UUID, UserSessionIssuerID: f.userIssuer}))
	rmcp := remotemcprepo.New(conn)
	_, err = rmcp.UpsertRemoteProtectedResource(ctx, remotemcprepo.UpsertRemoteProtectedResourceParams{
		ProjectID: f.projectID, OrganizationID: f.organizationID, ResourceIdentifier: upstream,
		MetadataUrl: "", AuthorizationServers: []string{"https://as-a.example.test"}, ScopesSupported: nil, BearerMethodsSupported: nil,
		ResourceName: "", ResourceDocumentation: "", ResourcePolicyUri: "", ResourceTosUri: "", Metadata: "",
	})
	require.NoError(t, err)
	_, err = rmcp.UpsertRemoteProtectedResourceScopeOverride(ctx, remotemcprepo.UpsertRemoteProtectedResourceScopeOverrideParams{ScopeOverride: []string{"files:write"}, ProjectID: f.projectID, OrganizationID: f.organizationID, ResourceIdentifier: upstream})
	require.NoError(t, err)

	binding, _ = call()
	require.Equal(t, []string{"files:write"}, binding.ConnectionScopes, "a linked client follows its server connection")
	require.Equal(t, []string{"read"}, binding.ConfiguredScopes)
	require.True(t, binding.ScopesOutOfDate, "the connection's scopes differ from the recorded ones")
}

func TestXAAIdentityChainingBindingConnectionScopesWire(t *testing.T) {
	t.Parallel()
	bound, err := json.Marshal(XAAIdentityChainingBinding{ConnectionScopes: []string{}})
	require.NoError(t, err)
	require.Contains(t, string(bound), `"connection_scopes":[]`, "a bound client recording no scope reports an empty set")
	unbound, err := json.Marshal(XAAIdentityChainingBinding{})
	require.NoError(t, err)
	require.NotContains(t, string(unbound), "connection_scopes", "omitted when no client is bound")

	schema, err := json.Marshal(inferOutputSchema[GetXAAReadinessOutput]("get_xaa_readiness"))
	require.NoError(t, err)
	require.Contains(t, string(schema), `"connection_scopes"`)
	require.Contains(t, string(schema), `"scopes_out_of_date"`)
}
