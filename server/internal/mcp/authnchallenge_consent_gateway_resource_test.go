package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcp_repo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
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

// newGatewayMemberUpstream serves an RFC 9728 protected-resource document
// naming authorizationServers, at every well-known candidate the discovery
// helper probes.
func newGatewayMemberUpstream(t *testing.T, authorizationServers ...string) *httptest.Server {
	t.Helper()
	quoted := make([]string, 0, len(authorizationServers))
	for _, as := range authorizationServers {
		quoted = append(quoted, `"`+as+`"`)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authorization_servers":[` + strings.Join(quoted, ",") + `]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newHangingUpstream never answers, releasing only when the probe's own
// deadline cancels the request.
func newHangingUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newUnreachableUpstreamURL returns a URL whose listener is already closed, so
// a probe against it fails immediately.
func newUnreachableUpstreamURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	url := srv.URL
	srv.Close()
	return url
}

// createGatewayMember attaches a remote-backed member, on its own user session
// issuer as a gateway member has in production, to metaServerID.
func createGatewayMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID, slug, serverURL string, sortOrder int32) {
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
		Visibility:          "public",
		UserSessionIssuerID: conv.ToNullUUID(createUserSessionIssuer(t, ctx, conn, projectID)),
	})
	require.NoError(t, err)

	_, err = metamcp_repo.New(conn).CreateMetaMCPMember(ctx, metamcp_repo.CreateMetaMCPMemberParams{
		ProjectID:       projectID,
		MetaMcpServerID: metaServerID,
		McpServerID:     mcpServer.ID,
		SortOrder:       sortOrder,
	})
	require.NoError(t, err)
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
// gateway rather than off any member.
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

// Each client's credential is qualified to the member whose protected-resource
// metadata names that client's authorization server — not to an arbitrary
// member, and not to nothing.
func TestServeConsentAction_GatewayConnectResolvesMemberByAuthorizationServer(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-resolve")

	asA := "https://aim87-a-as.example.com"
	asB := "https://aim87-b-as.example.com"
	memberA := newGatewayMemberUpstream(t, asA)
	memberB := newGatewayMemberUpstream(t, asB)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-member-a", memberA.URL+"/mcp", 0)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-member-b", memberB.URL+"/mcp", 1)

	clientA := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-a", asA, []uuid.UUID{fx.shared})
	clientB := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-b", asB, []uuid.UUID{fx.shared})

	locA := postConnectAction(t, fx, clientA)
	require.Equal(t, memberA.URL+"/mcp", locA.Query().Get("resource"))
	require.Equal(t, memberA.URL+"/mcp", mintedRemoteLoginState(t, ctx, fx, locA.Query().Get("state")).Resource)

	locB := postConnectAction(t, fx, clientB)
	require.Equal(t, memberB.URL+"/mcp", locB.Query().Get("resource"))
	require.Equal(t, memberB.URL+"/mcp", mintedRemoteLoginState(t, ctx, fx, locB.Query().Get("state")).Resource)
}

// No member claims the client's authorization server: fail closed rather than
// qualify the credential to a member it does not belong to.
func TestServeConsentAction_GatewayConnectNoMatchingMemberSendsNoResource(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-nomatch")

	member := newGatewayMemberUpstream(t, "https://aim87-other-as.example.com")
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-nomatch-member", member.URL+"/mcp", 0)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-nomatch", "https://aim87-unclaimed-as.example.com", []uuid.UUID{fx.shared})

	loc := postConnectAction(t, fx, client)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "no member claims this client's authorization server — send no resource")
	require.Empty(t, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// One authorization server fronting two members of the same gateway is
// unsupported in v1: a grant records one resource per (subject, client), so an
// ambiguous match fails closed.
func TestServeConsentAction_GatewayConnectAmbiguousMembersSendNoResource(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-ambiguous")

	shared := "https://aim87-shared-as.example.com"
	memberA := newGatewayMemberUpstream(t, shared)
	memberB := newGatewayMemberUpstream(t, shared)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-ambiguous-a", memberA.URL+"/mcp", 0)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-ambiguous-b", memberB.URL+"/mcp", 1)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-ambiguous", shared, []uuid.UUID{fx.shared})

	loc := postConnectAction(t, fx, client)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "two members behind one authorization server are ambiguous — send no resource")
	require.Empty(t, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// An unreachable member must not take the rest of the gateway down with it.
func TestServeConsentAction_GatewayConnectProbeFailureLeavesOtherMembersResolvable(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-probefail")

	as := "https://aim87-healthy-as.example.com"
	healthy := newGatewayMemberUpstream(t, as)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-broken-member", newUnreachableUpstreamURL(t)+"/mcp", 0)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-healthy-member", healthy.URL+"/mcp", 1)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-probefail", as, []uuid.UUID{fx.shared})

	loc := postConnectAction(t, fx, client)
	require.Equal(t, healthy.URL+"/mcp", loc.Query().Get("resource"))
	require.Equal(t, healthy.URL+"/mcp", mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// A member that never answers is bounded by the per-probe deadline, so consent
// resolves on the healthy member well inside the fan-out budget.
func TestServeConsentAction_GatewayConnectHangingMemberIsBounded(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-hang")

	as := "https://aim87-live-as.example.com"
	healthy := newGatewayMemberUpstream(t, as)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-hanging-member", newHangingUpstream(t).URL+"/mcp", 0)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-live-member", healthy.URL+"/mcp", 1)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-hang", as, []uuid.UUID{fx.shared})

	started := time.Now()
	loc := postConnectAction(t, fx, client)
	require.Equal(t, healthy.URL+"/mcp", loc.Query().Get("resource"))
	require.Less(t, time.Since(started), 5*time.Second, "the per-probe deadline, not the discovery helper's own budget, must bound one hung member")
}

// The gateway path is additive: an endpoint that is not a gateway keeps
// deriving its resource from the client's own attached MCP servers, even with
// a gateway in the project whose member would otherwise claim the client.
func TestServeConsentAction_NonGatewayEndpointKeepsPerClientDerivation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	projectID, orgID := consentTestTenant(t, ctx)

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	issuerA := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerA, "aim87-direct-srv", consentUpstreamA)

	as := "https://aim87-direct-as.example.com"
	metaServerID := createGatewayMetaServer(t, ctx, ti.conn, projectID, orgID, "aim87-unused-gw", shared)
	member := newGatewayMemberUpstream(t, as)
	createGatewayMember(t, ctx, ti.conn, projectID, metaServerID, "aim87-unused-member", member.URL+"/mcp", 0)

	endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, shared, "aim87-non-gateway")
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
	client := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "aim87-direct", as, []uuid.UUID{shared, issuerA})

	loc := postConnectAction(t, fx, client)
	require.Equal(t, consentUpstreamA, loc.Query().Get("resource"), "a non-gateway endpoint must keep the stored per-client derivation")
	require.Equal(t, consentUpstreamA, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}
