// Tunneled meta MCP members: execute_tool dispatches through the tunnel
// gateway with the caller's lone stored credential, and fails closed —
// making no tunnel forward — the moment the credential map turns ambiguous.
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
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// seedTunneledMetaMember creates a tunneled mcp_server (with its own issuer,
// per mcp_servers_issuer_required_check) and attaches it to the meta server,
// returning the tunnel id that routes are published under.
func seedTunneledMetaMember(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	projectID uuid.UUID,
	metaID uuid.UUID,
	name, slug string,
	sortOrder int32,
) uuid.UUID {
	t.Helper()

	tunneledID, err := uuid.NewV7()
	require.NoError(t, err)
	tunneledServer, err := tunneledmcprepo.New(ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:        tunneledID,
		ProjectID: projectID,
		Name:      "meta-tunnel-" + uuid.NewString()[:8],
		KeyHash:   uuid.NewString(),
		KeyPrefix: "gram_tunnel_test",
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
	return tunneledServer.ID
}

// A tunneled member records no RFC 8707 resource, so its credential is the
// caller's lone unqualified stored token — and execute_tool must carry
// exactly that token through the tunnel gateway on the wire.
func TestServePublic_MetaEndpoint_ExecuteTool_TunneledMemberForwardsLoneToken(t *testing.T) {
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
	tunnelID := seedTunneledMetaMember(t, ctx, ti, projectID, meta.ID, "Tunneled member", "member-tunnel", 0)
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), gatewayServer.URL, time.Hour))

	client := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-a", "", []uuid.UUID{sharedIssuerID})
	subject := urn.NewUserSubject("meta-tunnel-user-" + uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, client, subject, "token-tunnel", "")

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "tunneled member execute_tool must succeed: %s", text)
	require.Contains(t, text, "pong through the tunnel")
	forwarded := gateway.forwardFor(`"tools/call"`)
	require.Equal(t, "Bearer token-tunnel", forwarded.Get("Authorization"),
		"the lone empty-resource token must reach the tunnel gateway as the member's bearer")

	// A second stored token makes the credential unroutable — no recorded
	// resource can discriminate tunnels — so the call fails member-scoped
	// and the tunnel gateway sees no new forward.
	forwardsBefore := gateway.forwardCount()
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-tunnel-ambig-b", "", []uuid.UUID{sharedIssuerID})
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientB, subject, "token-elsewhere", "https://elsewhere.example.com/mcp")

	rpc = executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--ping")
	text, isError = metaToolResultText(t, rpc)
	require.True(t, isError, "an ambiguous credential map must fail the tunneled member call")
	require.Contains(t, text, "no upstream identity of its own", "the message must explain why a tunneled member cannot match a credential")
	require.Equal(t, forwardsBefore, gateway.forwardCount(), "the failed call must not produce any tunnel forward")
}
