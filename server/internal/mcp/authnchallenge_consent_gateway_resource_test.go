package mcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcp_repo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	tunneledmcp_repo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// consentTestTenant reads the project and organization the test service's auth
// context was seeded with.
func consentTestTenant(t *testing.T, ctx context.Context) (uuid.UUID, string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return *authCtx.ProjectID, authCtx.ActiveOrganizationID
}

// clientRemoteIssuerID reads back the remote_session_issuer a seeded client
// authenticates against, so a member can be stamped with the same row. Row
// identity is what the lookup matches on, not the issuer URL.
func clientRemoteIssuerID(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID string, clientID uuid.UUID) uuid.UUID {
	t.Helper()
	client, err := remotesessions_repo.New(conn).GetRemoteSessionClientByID(ctx, remotesessions_repo.GetRemoteSessionClientByIDParams{
		ID:             clientID,
		ProjectID:      projectID,
		OrganizationID: conv.ToPGText(organizationID),
	})
	require.NoError(t, err)
	return client.RemoteSessionClient.RemoteSessionIssuerID
}

// gatewayMember identifies a seeded member so a test can disable, detach, or
// tombstone it after the fact.
type gatewayMember struct {
	mcpServerID uuid.UUID
	memberID    uuid.UUID
}

// createGatewayMember attaches a remote-backed member to metaServerID. The
// member carries its own user session issuer, as one does in production, plus
// the remote session issuer its upstream authenticates against — the pair the
// dropped exclusivity CHECK used to forbid and the lookup depends on.
func createGatewayMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID, slug, serverURL string, remoteIssuerID uuid.NullUUID, sortOrder int32) gatewayMember {
	t.Helper()
	return createGatewayMemberWithVisibility(t, ctx, conn, projectID, metaServerID, slug, serverURL, remoteIssuerID, sortOrder, "public")
}

func createGatewayMemberWithVisibility(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID, slug, serverURL string, remoteIssuerID uuid.NullUUID, sortOrder int32, visibility string) gatewayMember {
	t.Helper()

	remoteServer, err := remotemcp_repo.New(conn).CreateServer(ctx, remotemcp_repo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		TransportType: "sse",
		Url:           serverURL,
	})
	require.NoError(t, err)

	mcpServer, err := mcpservers_repo.New(conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		RemoteMcpServerID:   conv.ToNullUUID(remoteServer.ID),
		Visibility:          visibility,
		UserSessionIssuerID: conv.ToNullUUID(createUserSessionIssuer(t, ctx, conn, projectID)),
	})
	require.NoError(t, err)
	stampRemoteSessionIssuer(t, ctx, conn, projectID, mcpServer.ID, remoteIssuerID)

	return gatewayMember{mcpServerID: mcpServer.ID, memberID: attachGatewayMember(t, ctx, conn, projectID, metaServerID, mcpServer.ID, sortOrder)}
}

// stampRemoteSessionIssuer writes the column the way the binding resync does.
// Server creation cannot set it — a server has no client bindings yet at that
// point — so tests stamp it after the fact, as production does.
func stampRemoteSessionIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, mcpServerID uuid.UUID, remoteIssuerID uuid.NullUUID) {
	t.Helper()
	if !remoteIssuerID.Valid {
		return
	}
	require.NoError(t, testrepo.New(conn).SetMcpServerRemoteSessionIssuerFixture(ctx, testrepo.SetMcpServerRemoteSessionIssuerFixtureParams{
		RemoteSessionIssuerID: remoteIssuerID,
		ID:                    mcpServerID,
		ProjectID:             projectID,
	}))
}

// createTunneledGatewayMember attaches a tunneled member stamped with an
// issuer. It has no upstream URL, so the lookup must not return it however its
// issuer matches.
func createTunneledGatewayMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID, slug string, remoteIssuerID uuid.NullUUID, sortOrder int32) gatewayMember {
	t.Helper()

	tunneled, err := tunneledmcp_repo.New(conn).CreateServer(ctx, tunneledmcp_repo.CreateServerParams{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      slug,
		KeyHash:   "hash-" + slug,
		KeyPrefix: "pfx",
	})
	require.NoError(t, err)

	mcpServer, err := mcpservers_repo.New(conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		TunneledMcpServerID: conv.ToNullUUID(tunneled.ID),
		Visibility:          "public",
		UserSessionIssuerID: conv.ToNullUUID(createUserSessionIssuer(t, ctx, conn, projectID)),
	})
	require.NoError(t, err)
	stampRemoteSessionIssuer(t, ctx, conn, projectID, mcpServer.ID, remoteIssuerID)

	return gatewayMember{mcpServerID: mcpServer.ID, memberID: attachGatewayMember(t, ctx, conn, projectID, metaServerID, mcpServer.ID, sortOrder)}
}

func attachGatewayMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID, mcpServerID uuid.UUID, sortOrder int32) uuid.UUID {
	t.Helper()
	member, err := metamcp_repo.New(conn).CreateMetaMCPMember(ctx, metamcp_repo.CreateMetaMCPMemberParams{
		ProjectID:       projectID,
		MetaMcpServerID: metaServerID,
		McpServerID:     mcpServerID,
		SortOrder:       sortOrder,
	})
	require.NoError(t, err)
	return member.ID
}

// createGatewayMetaServer creates the meta_mcp_servers row a gateway endpoint
// resolves through.
func createGatewayMetaServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID, name string, userSessionIssuerID uuid.UUID) uuid.UUID {
	t.Helper()
	metaServer, err := metamcp_repo.New(conn).CreateMetaMCPServer(ctx, metamcp_repo.CreateMetaMCPServerParams{
		OrganizationID:      organizationID,
		ProjectID:           projectID,
		Name:                name,
		UserSessionIssuerID: conv.ToNullUUID(userSessionIssuerID),
	})
	require.NoError(t, err)
	return metaServer.ID
}

// seedGatewayConsentEndpoint builds a gateway endpoint over one shared user
// session issuer, so every client the consent screen offers hangs off the
// gateway rather than off any member. That is exactly why the stored
// derivation finds nothing and the member lookup has to answer.
func seedGatewayConsentEndpoint(t *testing.T, slug string) (context.Context, consentActionFixture, uuid.UUID) {
	t.Helper()

	ctx, ti := newTestMCPService(t)
	projectID, orgID := consentTestTenant(t, ctx)

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaServerID := createGatewayMetaServer(t, ctx, ti.conn, projectID, orgID, slug, shared)
	endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, shared, slug)
	endpoint.MetaMcpServerID = conv.ToNullUUID(metaServerID)

	return ctx, consentActionFixture{
		ti:        ti,
		endpoint:  endpoint,
		stateID:   stateID,
		projectID: projectID,
		orgID:     orgID,
		shared:    shared,
		subject:   subject,
		clientA:   uuid.Nil,
		clientB:   uuid.Nil,
		clientC:   uuid.Nil,
		clientD:   uuid.Nil,
	}, metaServerID
}

// The member whose upstream authenticates against the connecting client's
// authorization server is the one that qualifies the credential.
func TestServeConsentAction_GatewayConnectResolvesMemberByStoredIssuer(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-lookup-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-lookup", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	const matched = "https://matched.example.com/mcp"
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-lookup-match", matched, conv.ToNullUUID(issuerID), 0)
	// A sibling on a different authorization server must not be chosen.
	other := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-lookup-other", "", []uuid.UUID{fx.shared})
	otherIssuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, other)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-lookup-sibling", "https://sibling.example.com/mcp", conv.ToNullUUID(otherIssuerID), 1)

	loc := postConnectAction(t, fx, clientID)
	require.Equal(t, matched, loc.Query().Get("resource"), "the credential must be qualified to the member behind its own authorization server")
	require.Equal(t, matched, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource, "and the resolved member must survive onto the login state")
}

// An unstamped member cannot be matched, which is the pre-backfill state: the
// lookup finds nothing and the endpoint behaves exactly as it does today.
func TestServeConsentAction_GatewayConnectUnstampedMemberSendsNoResource(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-null-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-null", "", []uuid.UUID{fx.shared})
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-null-member", "https://unstamped.example.com/mcp", uuid.NullUUID{}, 0)

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a NULL remote_session_issuer_id must match nothing rather than matching everything")
	require.Empty(t, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// One authorization server fronting two members of the same gateway is
// unsupported: a grant records one resource per (subject, client), so there is
// no value that routes both correctly.
func TestServeConsentAction_GatewayConnectSharedIssuerSendsNoResource(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-shared-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-shared", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-shared-a", "https://shared-a.example.com/mcp", conv.ToNullUUID(issuerID), 0)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-shared-b", "https://shared-b.example.com/mcp", conv.ToNullUUID(issuerID), 1)

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "two members behind one authorization server must fail closed, not pick one")
	require.Empty(t, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// Two members fronting one URL are one routing destination, so they collapse
// rather than reading as ambiguous: routeUpstreamToken keys on the URL.
func TestServeConsentAction_GatewayConnectMembersSharingOneURLCollapse(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-dupe-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-dupe", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	const upstream = "https://dupe.example.com/mcp"
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-dupe-a", upstream, conv.ToNullUUID(issuerID), 0)
	// The trailing slash is the same destination once routing trims it.
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-dupe-b", upstream+"/", conv.ToNullUUID(issuerID), 1)

	require.Equal(t, upstream, postConnectAction(t, fx, clientID).Query().Get("resource"))
}

// A member the caller cannot connect to must neither claim their credential —
// the resolved URL is echoed back and sent to the authorization server — nor
// contest the claim of a member they can reach.
func TestServeConsentAction_GatewayConnectExcludesMembersTheSubjectCannotConnectTo(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-rbac-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-rbac", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	// Private, and the anonymous consent subject holds no mcp:connect on it.
	createGatewayMemberWithVisibility(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-rbac-private", "https://private.example.com/mcp", conv.ToNullUUID(issuerID), 0, "private")

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "an unreachable member must not claim the credential")

	// The same member, now public, does resolve — so the exclusion above was
	// the authorization check and not the lookup failing to see it at all.
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-rbac-public", "https://public.example.com/mcp", conv.ToNullUUID(issuerID), 1)
	require.Equal(t, "https://public.example.com/mcp", postConnectAction(t, fx, clientID).Query().Get("resource"))
}

// A disabled member does not exist for the serving path, so it must not
// qualify a credential either.
func TestServeConsentAction_GatewayConnectIgnoresDisabledMembers(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-disabled-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-disabled", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	createGatewayMemberWithVisibility(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-disabled-member", "https://disabled.example.com/mcp", conv.ToNullUUID(issuerID), 0, "disabled")

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a disabled member must not qualify a credential")
}

// A tunneled member has no upstream URL, so there is nothing a token could be
// routed to however well its issuer matches.
func TestServeConsentAction_GatewayConnectExcludesTunneledMembers(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-tunnel-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-tunnel", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	createTunneledGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-tunnel-member", conv.ToNullUUID(issuerID), 0)

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a tunneled member advertises no upstream URL to route to")
}

// A member of another gateway, on the same issuer, must not be reachable from
// this endpoint.
func TestServeConsentAction_GatewayConnectIgnoresAnotherGatewaysMembers(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-scope-gw")

	clientID := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-scope", "", []uuid.UUID{fx.shared})
	issuerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, clientID)

	otherGateway := createGatewayMetaServer(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-scope-other", createUserSessionIssuer(t, ctx, fx.ti.conn, fx.projectID))
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, otherGateway, "aim87-scope-elsewhere", "https://elsewhere.example.com/mcp", conv.ToNullUUID(issuerID), 0)

	loc := postConnectAction(t, fx, clientID)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "the lookup must be scoped to this gateway's members")

	// An equivalent member attached to this gateway does resolve, so the
	// exclusion above was the scoping and not the stamp being unreadable.
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-scope-mine", "https://mine.example.com/mcp", conv.ToNullUUID(issuerID), 1)
	require.Equal(t, "https://mine.example.com/mcp", postConnectAction(t, fx, clientID).Query().Get("resource"))
}

// A non-gateway endpoint keeps the stored per-client derivation untouched.
func TestServeConsentAction_NonGatewayEndpointKeepsPerClientDerivation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	projectID, orgID := consentTestTenant(t, ctx)

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	memberIssuer := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, memberIssuer, "aim87-plain-srv", consentUpstreamA)

	endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, shared, "aim87-plain")
	fx := consentActionFixture{
		ti:        ti,
		endpoint:  endpoint,
		stateID:   stateID,
		projectID: projectID,
		orgID:     orgID,
		shared:    shared,
		subject:   subject,
		clientA:   uuid.Nil,
		clientB:   uuid.Nil,
		clientC:   uuid.Nil,
		clientD:   uuid.Nil,
	}

	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "aim87-plain", "", []uuid.UUID{shared, memberIssuer})
	require.Equal(t, consentUpstreamA, postConnectAction(t, fx, clientID).Query().Get("resource"))
}
