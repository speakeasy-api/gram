// Tunneled meta MCP members: execute_tool dispatches through the tunnel
// gateway with the stored credential keyed by the member's own derived
// remote_session_issuer, and degrades to an anonymous call — never a
// sibling's bearer — when that entry is absent.
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
	"github.com/speakeasy-api/gram/server/internal/urn"
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

	// The member's provider client is bound to the gateway's issuer (so the
	// session can hold its credential) and to the member's own issuer (so the
	// resync derives the mcp_servers.remote_session_issuer_id routing key).
	client := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-a", "", []uuid.UUID{sharedIssuerID, memberIssuerID})
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{memberIssuerID}))
	subject := urn.NewUserSubject("meta-tunnel-user-" + uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, client, subject, "token-tunnel", "")

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "tunneled member execute_tool must succeed: %s", text)
	require.Contains(t, text, "pong through the tunnel")
	forwarded := gateway.forwardFor(`"tools/call"`)
	require.Equal(t, "Bearer token-tunnel", forwarded.Get("Authorization"),
		"the member's own keyed token must reach the tunnel gateway as its bearer")

	// A sibling credential for some other upstream must not disturb routing:
	// the member still forwards its own keyed token.
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-sibling-b", "", []uuid.UUID{sharedIssuerID})
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

	client := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-rid", "", []uuid.UUID{sharedIssuerID, memberIssuerID})
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{memberIssuerID}))
	subject := urn.NewUserSubject("meta-tunnel-rid-user-" + uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, client, subject, "token-qualified", identifier)

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

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
	createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-own", "", []uuid.UUID{sharedIssuerID, memberIssuerID})
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{memberIssuerID}))

	// The victim credential belongs to a different provider client, minted
	// against the vendor's authorization server and correctly audienced to
	// it — so a resource scan would match the tunnel's claimed identifier.
	vendorClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-vendor", "", []uuid.UUID{sharedIssuerID})
	subject := urn.NewUserSubject("meta-tunnel-harvest-user-" + uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, vendorClient, subject, "victim-vendor-token", vendorResource)

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "the member must still serve, anonymously: %s", text)
	require.Empty(t, gateway.forwardFor(`"tools/call"`).Get("Authorization"),
		"a credential minted through another authorization server must never reach the tunnel")
}

// The partial-resolution leak guard: a tunneled member whose own provider the
// subject never connected must be called anonymously, even when the session
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
	clientA := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-leak-a", "", []uuid.UUID{sharedIssuerID, memberAIssuerID})
	createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-leak-b", "", []uuid.UUID{sharedIssuerID, memberBIssuerID})
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{memberAIssuerID, memberBIssuerID}))
	subject := urn.NewUserSubject("meta-tunnel-leak-user-" + uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientA, subject, "token-member-a", "")

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel-a--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "the connected member must serve: %s", text)
	require.Equal(t, "Bearer token-member-a", gatewayA.forwardFor(`"tools/call"`).Get("Authorization"),
		"the connected member must receive its own bearer")

	rpc = executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel-b--ping")
	text, isError = metaToolResultText(t, rpc)
	require.False(t, isError, "the unconnected member must degrade to an anonymous call, not fail: %s", text)
	require.Empty(t, gatewayB.forwardFor(`"tools/call"`).Get("Authorization"),
		"member A's bearer must never reach member B's tunnel")
}
