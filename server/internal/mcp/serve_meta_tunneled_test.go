// Tunneled meta MCP members: execute_tool dispatches through the tunnel
// gateway with the stored credential keyed by the member's own derived
// auth client. A missing connection refuses that member rather than sending
// no bearer or a sibling's bearer.
// The fake tunnel gateway answers the handshake-first initialize (minting a
// backend Mcp-Session-Id) so the full per-call session lifecycle runs.
package mcp_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// seedTunneledMetaMember creates a tunneled mcp_server (with its own issuer,
// per mcp_servers_issuer_required_check) and attaches it to the meta server,
// returning the tunnel id that routes are published under and the member's
// own user_session_issuer id (the input to the issuer resync that token
// routing keys on). A non-empty resourceIdentifier is recorded on the source
// at creation, the way an operator who already knows it records it.
func seedTunneledMetaMember(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	projectID uuid.UUID,
	metaID uuid.UUID,
	name, slug string,
	sortOrder int32,
	resourceIdentifier string,
) (uuid.UUID, uuid.UUID) {
	t.Helper()

	tunnelName := "meta-tunnel-" + uuid.NewString()[:8]
	tunneledID, err := uuid.NewV7()
	require.NoError(t, err)
	tunneledServer, err := tunneledmcprepo.New(ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:                 tunneledID,
		ProjectID:          projectID,
		Name:               tunnelName,
		KeyHash:            uuid.NewString(),
		KeyPrefix:          "gram_tunnel_test",
		ResourceIdentifier: conv.ToPGTextEmpty(resourceIdentifier),
	})
	require.NoError(t, err)

	memberIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  serverID,
		ProjectID:           projectID,
		Name:                conv.ToPGText(name),
		Slug:                conv.ToPGText(slug),
		EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID: uuid.NullUUID{UUID: memberIssuerID, Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID: uuid.NullUUID{UUID: tunneledServer.ID, Valid: true},
		ToolsetID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:          mcpservers.VisibilityPublic,
	})
	require.NoError(t, err)

	_, err = metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID:       projectID,
		MetaMcpServerID: metaID,
		McpServerID:     server.ID,
		SortOrder:       sortOrder,
	})
	require.NoError(t, err)
	return tunneledServer.ID, memberIssuerID
}

func tunneledFixtureMember(t *testing.T, ctx context.Context, ti *testInstance, projectID, gatewayID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	members, err := metamcprepo.New(ti.conn).ListMetaMCPMembers(ctx, metamcprepo.ListMetaMCPMembersParams{MetaMcpServerID: gatewayID, ProjectID: projectID})
	require.NoError(t, err)
	for _, member := range members {
		if member.McpServerSlug.String == slug {
			return member.McpServerID
		}
	}
	t.Fatalf("test gateway has no member %s", slug)
	return uuid.Nil
}

// A tunneled member records no RFC 8707 resource, so its credential is the
// stored token keyed by the member's own derived remote_session_issuer — and
// execute_tool must carry exactly that token through the tunnel gateway on
// the wire, no matter what sibling credentials the session also holds.
func TestServePublic_MetaEndpoint_ExecuteTool_TunneledMemberRoutesOwnToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-tunnel-e2e-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: false, dead: false, challenge: ""}
	tunnelID, memberIssuerID := seedTunneledMetaMember(t, ctx, ti, projectID, meta.ID, "Tunneled member", "member-tunnel", 0, "")
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), gatewayServer.URL, time.Hour))

	// The member's scoped placement supplies its routing key. Its typed offer
	// supplies the credential choice for the gateway session.
	client := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-a", "")
	subject := createTestUser(t, ctx, ti, "meta-tunnel-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, client, subject, "token-tunnel", "")

	member := tunneledFixtureMember(t, ctx, ti, projectID, meta.ID, "member-tunnel")
	associateServerRegistration(t, ctx, ti.conn, orgID, projectID, member, client)
	typed := seedTypedSessionClient(t, ctx, ti, orgID, projectID, member, client, "remote_session", subject)
	require.NoError(t, remotesessions.RestampMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{memberIssuerID}))
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject, metaServingChoice{organizationID: orgID, serverID: member, clientID: typed})

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "tunneled member execute_tool must succeed: %s", text)
	require.Contains(t, text, "pong through the tunnel")
	forwarded := gateway.forwardFor(`"tools/call"`)
	require.Equal(t, "Bearer token-tunnel", forwarded.Get("Authorization"),
		"the member's own keyed token must reach the tunnel gateway as its bearer")
	// The tunneled member's tool_call row carries the gateway id.
	requireTelemetryRowCount(t, `meta_mcp_server_id = ? AND event_source = 'tool_call' AND tool_name = 'ping'`, 1, meta.ID.String())

	// A sibling credential for some other upstream must not disturb routing:
	// the member still forwards its own keyed token.
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-sibling-b", "")
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientB, subject, "token-elsewhere", "https://elsewhere.example.com/mcp")

	rpc = executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--ping")
	text, isError = metaToolResultText(t, rpc)
	require.False(t, isError, "a sibling credential must not break the tunneled member call: %s", text)
	forwarded = gateway.forwardFor(`"tools/call"`)
	require.Equal(t, "Bearer token-tunnel", forwarded.Get("Authorization"),
		"the member must keep forwarding its own token, never a sibling's")
}

// A tunneled member that records a resource identifier accepts the grant
// minted against its own authorization server and qualified to that
// identifier — the whole point of recording one — and carries it through the
// tunnel gateway on the wire.
func TestServePublic_MetaEndpoint_ExecuteTool_TunneledMemberRoutesIdentifierQualifiedToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	const identifier = "https://tunneled.internal/mcp"

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-tunnel-rid-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-rid", backendSessionID: "backend-rid", legacy: false, dead: false, challenge: ""}
	tunnelID, memberIssuerID := seedTunneledMetaMember(t, ctx, ti, projectID, meta.ID, "Tunneled member", "member-tunnel", 0, identifier)
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), gatewayServer.URL, time.Hour))

	client := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-rid", "")
	subject := createTestUser(t, ctx, ti, "meta-tunnel-rid-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, client, subject, "token-qualified", identifier)

	member := tunneledFixtureMember(t, ctx, ti, projectID, meta.ID, "member-tunnel")
	associateServerRegistration(t, ctx, ti.conn, orgID, projectID, member, client)
	typed := seedTypedSessionClient(t, ctx, ti, orgID, projectID, member, client, "remote_session", subject)
	require.NoError(t, remotesessions.RestampMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{memberIssuerID}))
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject, metaServingChoice{organizationID: orgID, serverID: member, clientID: typed})

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "a grant qualified to the member's identifier must serve: %s", text)
	require.Equal(t, "Bearer token-qualified", gateway.forwardFor(`"tools/call"`).Get("Authorization"))
}

// A recorded identifier selects among grants under the member's own issuer;
// it never reaches across issuers. Otherwise an operator holding mcp:write
// could name a sibling's upstream as their tunnel's identifier and have every
// user's credential for that upstream delivered into the tunnel.
func TestServePublic_MetaEndpoint_ExecuteTool_TunneledIdentifierNeverHarvestsSiblingToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	// The tunnel claims the sibling's audience as its own identifier.
	const vendorResource = "https://api.vendor.example.com/mcp"

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-tunnel-harvest-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-h", backendSessionID: "backend-h", legacy: false, dead: false, challenge: ""}
	tunnelID, memberIssuerID := seedTunneledMetaMember(t, ctx, ti, projectID, meta.ID, "Tunneled member", "member-tunnel", 0, vendorResource)
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), gatewayServer.URL, time.Hour))

	// The member has a provider of its own, so it does resolve a derived
	// issuer to key on — the routing decision is about which entry it may
	// take, not about the member being unroutable. The subject never
	// connected that provider, so the map holds no entry under it.
	ownClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-own", "")

	// The victim credential belongs to a different provider client, minted
	// against the vendor's authorization server and correctly audienced to
	// it — so a resource scan would match the tunnel's claimed identifier.
	vendorClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-vendor", "")
	subject := createTestUser(t, ctx, ti, "meta-tunnel-harvest-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, vendorClient, subject, "victim-vendor-token", vendorResource)

	member := tunneledFixtureMember(t, ctx, ti, projectID, meta.ID, "member-tunnel")
	associateServerRegistration(t, ctx, ti.conn, orgID, projectID, member, ownClient)
	typed := seedTypedSessionClient(t, ctx, ti, orgID, projectID, member, ownClient, "remote_session", subject)
	require.NoError(t, remotesessions.RestampMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{memberIssuerID}))
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject, metaServingChoice{organizationID: orgID, serverID: member, clientID: typed})

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--ping")
	text, isError := metaToolResultText(t, rpc)
	require.True(t, isError, "a chosen member with no usable connection must fail closed: %s", text)
	require.Contains(t, text, "is not connected for this user")
	require.Zero(t, gateway.forwardCount(), "no tool call dials the tunnel with a different credential")
}

// The partial-resolution leak guard: a tunneled member whose own provider the
// subject never connected must not dial, even when the session
// holds another tunneled sibling's unqualified credential — the exact map
// shape that a lone-token fallback would misroute.
func TestServePublic_MetaEndpoint_ExecuteTool_TunneledMemberNeverBorrowsSiblingToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-tunnel-leak-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	gatewayA := &fakeTunnelGateway{t: t, agentSessionID: "agent-a", backendSessionID: "backend-a", legacy: false, dead: false, challenge: ""}
	gatewayB := &fakeTunnelGateway{t: t, agentSessionID: "agent-b", backendSessionID: "backend-b", legacy: false, dead: false, challenge: ""}
	tunnelAID, memberAIssuerID := seedTunneledMetaMember(t, ctx, ti, projectID, meta.ID, "Tunneled member A", "member-tunnel-a", 0, "")
	tunnelBID, memberBIssuerID := seedTunneledMetaMember(t, ctx, ti, projectID, meta.ID, "Tunneled member B", "member-tunnel-b", 1, "")
	serverA := httptest.NewServer(gatewayA)
	t.Cleanup(serverA.Close)
	serverB := httptest.NewServer(gatewayB)
	t.Cleanup(serverB.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelAID.String(), serverA.URL, time.Hour))
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelBID.String(), serverB.URL, time.Hour))

	// Both members' providers are attached to the gateway, but the subject
	// connected only member A's — so the resolved map holds exactly one
	// unqualified credential, and it is not member B's.
	clientA := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-leak-a", "")
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-leak-b", "")
	subject := createTestUser(t, ctx, ti, "meta-tunnel-leak-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientA, subject, "token-member-a", "")

	member := tunneledFixtureMember(t, ctx, ti, projectID, meta.ID, "member-tunnel-a")
	memberB := tunneledFixtureMember(t, ctx, ti, projectID, meta.ID, "member-tunnel-b")
	associateServerRegistration(t, ctx, ti.conn, orgID, projectID, member, clientA)
	associateServerRegistration(t, ctx, ti.conn, orgID, projectID, memberB, clientB)
	typed := seedTypedSessionClient(t, ctx, ti, orgID, projectID, member, clientA, "remote_session", subject)
	seedServerAuthOffer(t, ctx, ti.conn, orgID, projectID, memberB, clientB, "remote_session")
	require.NoError(t, remotesessions.RestampMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{memberAIssuerID, memberBIssuerID}))
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject, metaServingChoice{organizationID: orgID, serverID: member, clientID: typed})

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel-a--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "the connected member must serve: %s", text)
	require.Equal(t, "Bearer token-member-a", gatewayA.forwardFor(`"tools/call"`).Get("Authorization"),
		"the connected member must receive its own bearer")

	rpc = executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel-b--ping")
	require.NotEmpty(t, rpc["error"], "the unconnected member fails closed")
	require.Zero(t, gatewayB.forwardCount(), "member B must not dial with another member's connection")
}
