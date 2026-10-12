package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
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
	q := remotesessionsrepo.New(conn)
	client, err := q.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		OrganizationID: conv.ToPGText(f.organizationID), ProjectID: conv.ToNullUUID(f.projectID), RemoteSessionIssuerID: issuer, ClientID: "resource-" + uuid.NewString()[:8],
		ClientIDIssuedAt: conv.ToPGTimestamptz(time.Now().UTC()), TokenEndpointAuthMethod: conv.ToPGText("none"), Scope: []string{},
	})
	require.NoError(t, err)
	key := remotesessionsrepo.GetEMABindingParams{ProjectID: f.projectID, OrganizationID: f.organizationID, UserSessionIssuerID: f.userIssuer, RemoteSessionIssuerID: issuer, Resource: "https://upstream.example.test/mcp"}
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

// orderedGovernor also reports the per-vendor identity provider order.
type orderedGovernor struct {
	*recordingGovernor
	kinds []remotesessions.EMAProviderKind
}

func (g orderedGovernor) Providers(context.Context, identitychaining.Request) []remotesessions.EMAProviderKind {
	return g.kinds
}

func TestXAAIdentityInspectorReportsProviderOrder(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_xaa_inspector_providers")
	require.NoError(t, err)
	f := seedXAAInspectorFixture(t, ctx, conn, pgtype.Text{})
	bindXAAInspectorUpstream(t, ctx, conn, f, f.issuerA)

	governor := orderedGovernor{recordingGovernor: &recordingGovernor{issuer: f.issuerA, serves: true}, kinds: []remotesessions.EMAProviderKind{remotesessions.EMAProviderSpeakeasy, remotesessions.EMAProviderExternal}}
	inspector := &postgresXAAIdentityInspector{db: conn, governor: governor, origins: remotesessions.CallbackOrigins{Outbound: nil, Registration: nil}}
	chaining, _, err := inspector.Inspect(ctx, f.organizationID, f.projectID, f.serverID, f.issuerA, "https://upstream.example.test/mcp")
	require.NoError(t, err)
	require.True(t, chaining.Served)
	require.Equal(t, []string{"speakeasy", "external"}, chaining.Providers)

	unserved := orderedGovernor{recordingGovernor: &recordingGovernor{issuer: f.issuerA, serves: false}, kinds: []remotesessions.EMAProviderKind{remotesessions.EMAProviderExternal}}
	inspector.governor = unserved
	chaining, _, err = inspector.Inspect(ctx, f.organizationID, f.projectID, f.serverID, f.issuerA, "https://upstream.example.test/mcp")
	require.NoError(t, err)
	require.Empty(t, chaining.Providers, "no order is reported when chaining does not serve the upstream")
}
