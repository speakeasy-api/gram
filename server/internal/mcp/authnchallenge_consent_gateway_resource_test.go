package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcp_repo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	tunneledmcp_repo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	unproxiedmcp_repo "github.com/speakeasy-api/gram/server/internal/unproxiedmcp/repo"
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
// helper probes. The document omits the optional `resource` field, which is
// the shape a member that makes no claim about its own identity returns.
func newGatewayMemberUpstream(t *testing.T, authorizationServers ...string) *httptest.Server {
	t.Helper()
	return newGatewayMemberUpstreamDeclaring(t, "", authorizationServers...)
}

// newGatewayMemberUpstreamDeclaring is newGatewayMemberUpstream with the
// document's RFC 9728 `resource` field under test control, so a document that
// names a resource other than the member serving it can be seeded. That is
// what a shared multi-tenant host returns from its origin-style location when
// the member's own path-style probe 404s.
func newGatewayMemberUpstreamDeclaring(t *testing.T, declaredResource string, authorizationServers ...string) *httptest.Server {
	t.Helper()
	quoted := make([]string, 0, len(authorizationServers))
	for _, as := range authorizationServers {
		quoted = append(quoted, `"`+as+`"`)
	}
	resourceField := ""
	if declaredResource != "" {
		resourceField = `"resource":"` + declaredResource + `",`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{` + resourceField + `"authorization_servers":[` + strings.Join(quoted, ",") + `]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newGatewayMemberUpstreamDeclaringSelf serves a document whose resource field
// names this server's own URL plus path — the compliant shape. path may carry
// a trailing slash the member's stored URL does not, so the positive case only
// passes under the same canonicalisation the authorization-server match uses.
func newGatewayMemberUpstreamDeclaringSelf(t *testing.T, path string, authorizationServers ...string) *httptest.Server {
	t.Helper()
	quoted := make([]string, 0, len(authorizationServers))
	for _, as := range authorizationServers {
		quoted = append(quoted, `"`+as+`"`)
	}
	var self atomic.Value
	self.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource") {
			http.NotFound(w, r)
			return
		}
		declared, _ := self.Load().(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"` + declared + `","authorization_servers":[` + strings.Join(quoted, ",") + `]}`))
	}))
	self.Store(srv.URL + path)
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

// newToggleableGatewayMemberUpstream advertises its authorization server only
// while advertising is set, so one member can stop being discoverable between
// a connect and a reconnect.
func newToggleableGatewayMemberUpstream(t *testing.T, advertising *atomic.Bool, authorizationServer string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !advertising.Load() || !strings.HasPrefix(r.URL.Path, "/.well-known/oauth-protected-resource") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authorization_servers":["` + authorizationServer + `"]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newUpstreamWithoutMetadata answers 404 everywhere, as an upstream that does
// not speak OAuth does. Its probe fails cleanly rather than hanging.
func newUpstreamWithoutMetadata(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
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

// gatewayMember identifies a seeded member so a test can disable, detach, or
// tombstone it after the fact.
type gatewayMember struct {
	mcpServerID uuid.UUID
	memberID    uuid.UUID
}

// createGatewayMember attaches a remote-backed member, on its own user session
// issuer as a gateway member has in production, to metaServerID.
func createGatewayMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID, slug, serverURL string, sortOrder int32) gatewayMember {
	t.Helper()
	return createGatewayMemberWithVisibility(t, ctx, conn, projectID, metaServerID, slug, serverURL, sortOrder, "public")
}

// createGatewayMemberWithVisibility is createGatewayMember with the member
// server's visibility under test control, so a disabled member can be seeded.
func createGatewayMemberWithVisibility(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID, slug, serverURL string, sortOrder int32, visibility string) gatewayMember {
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

	return gatewayMember{mcpServerID: mcpServer.ID, memberID: attachGatewayMember(t, ctx, conn, projectID, metaServerID, mcpServer.ID, sortOrder)}
}

// attachGatewayMember writes the meta_mcp_server_members row joining an
// existing mcp_server to a gateway.
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

// createTunneledGatewayMember attaches a tunneled-backed member: a real member
// class in v0 that advertises no metadata document of its own.
func createTunneledGatewayMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID, slug string, sortOrder int32) gatewayMember {
	t.Helper()

	tunneled, err := tunneledmcp_repo.New(conn).CreateServer(ctx, tunneledmcp_repo.CreateServerParams{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      slug,
		KeyHash:   uuid.NewString(),
		KeyPrefix: "gram_tunnel_test",
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

	return gatewayMember{mcpServerID: mcpServer.ID, memberID: attachGatewayMember(t, ctx, conn, projectID, metaServerID, mcpServer.ID, sortOrder)}
}

// createToolsetGatewayMember attaches a Gram-hosted (toolset-backed) member.
func createToolsetGatewayMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID, slug string, sortOrder int32) gatewayMember {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(conn), authCtx, slug+"-toolset")

	mcpServer, err := mcpservers_repo.New(conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		ToolsetID:           conv.ToNullUUID(toolset.ID),
		Visibility:          "public",
		UserSessionIssuerID: conv.ToNullUUID(createUserSessionIssuer(t, ctx, conn, projectID)),
	})
	require.NoError(t, err)

	return gatewayMember{mcpServerID: mcpServer.ID, memberID: attachGatewayMember(t, ctx, conn, projectID, metaServerID, mcpServer.ID, sortOrder)}
}

// createUnproxiedGatewayMember attaches an unproxied member. It carries a URL
// like a remote member does, so it is the sharpest check that the upstream
// query selects on the backend join and not on the presence of a URL.
func createUnproxiedGatewayMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID, slug, serverURL string, sortOrder int32) gatewayMember {
	t.Helper()

	unproxied, err := unproxiedmcp_repo.New(conn).CreateServer(ctx, unproxiedmcp_repo.CreateServerParams{
		ID:          uuid.New(),
		ProjectID:   projectID,
		Name:        conv.ToPGText(slug),
		Slug:        conv.ToPGText(slug),
		Url:         serverURL,
		Description: conv.ToPGText(""),
	})
	require.NoError(t, err)

	mcpServer, err := mcpservers_repo.New(conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                   uuid.New(),
		ProjectID:            projectID,
		Name:                 conv.ToPGText(slug),
		Slug:                 conv.ToPGText(slug),
		UnproxiedMcpServerID: conv.ToNullUUID(unproxied.ID),
		Visibility:           "public",
		UserSessionIssuerID:  conv.ToNullUUID(createUserSessionIssuer(t, ctx, conn, projectID)),
	})
	require.NoError(t, err)

	return gatewayMember{mcpServerID: mcpServer.ID, memberID: attachGatewayMember(t, ctx, conn, projectID, metaServerID, mcpServer.ID, sortOrder)}
}

// gatewayMemberUpstreamURLs reads back what the consent-time query considers
// probeable. Tests assert on it so a "no resource" outcome can never pass
// vacuously on an empty candidate set.
func gatewayMemberUpstreamURLs(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, metaServerID uuid.UUID) []string {
	t.Helper()
	rows, err := metamcp_repo.New(conn).ListServableMetaMCPMemberUpstreams(ctx, metamcp_repo.ListServableMetaMCPMemberUpstreamsParams{
		MetaMcpServerID: metaServerID,
		ProjectID:       projectID,
	})
	require.NoError(t, err)
	urls := make([]string, 0, len(rows))
	for _, row := range rows {
		urls = append(urls, row.UpstreamUrl)
	}
	return urls
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

	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), 1, "the member must be a probe candidate, or this asserts nothing")

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

	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), 2, "both members must be probe candidates, or this asserts nothing")

	loc := postConnectAction(t, fx, client)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "two members behind one authorization server are ambiguous — send no resource")
	require.Empty(t, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// A member Gram could not reach has not said it claims nothing — nobody could
// ask it. A member that answers 404 has. Only the second may be treated as
// making no claim, and the pair is what separates the two.
func TestServeConsentAction_GatewayConnectUnreachableMemberPoisonsButA404DoesNot(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-probefail")

	as := "https://aim87-healthy-as.example.com"
	healthy := newGatewayMemberUpstream(t, as)
	unreachable := createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-broken-member", newUnreachableUpstreamURL(t)+"/mcp", 0)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-healthy-member", healthy.URL+"/mcp", 1)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-probefail", as, []uuid.UUID{fx.shared})

	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), 2, "both members must be probe candidates, or this asserts nothing")

	loc := postConnectAction(t, fx, client)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a member whose metadata could not be read leaves every other member's claim unproven")
	require.Empty(t, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)

	// Same gateway, same healthy member: swap the silent member for one that
	// answers 404 and the resolution goes through.
	_, err := metamcp_repo.New(fx.ti.conn).DeleteMetaMCPMember(ctx, metamcp_repo.DeleteMetaMCPMemberParams{ID: unreachable.memberID, ProjectID: fx.projectID})
	require.NoError(t, err)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-quiet-member", newUpstreamWithoutMetadata(t).URL+"/mcp", 2)
	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), 2, "the 404 member must be a probe candidate, or this asserts nothing")

	loc = postConnectAction(t, fx, client)
	require.Equal(t, healthy.URL+"/mcp", loc.Query().Get("resource"), "a member without OAuth answers 404, and an answer is not an absence")
	require.Equal(t, healthy.URL+"/mcp", mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// A member that never answers is bounded by the per-probe deadline — and,
// having never answered, leaves the healthy member's claim unproven.
func TestServeConsentAction_GatewayConnectHangingMemberIsBoundedAndPoisons(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-hang")

	as := "https://aim87-live-as.example.com"
	healthy := newGatewayMemberUpstream(t, as)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-hanging-member", newHangingUpstream(t).URL+"/mcp", 0)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-live-member", healthy.URL+"/mcp", 1)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-hang", as, []uuid.UUID{fx.shared})

	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), 2, "both members must be probe candidates, or this asserts nothing")

	started := time.Now()
	loc := postConnectAction(t, fx, client)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a member that never answers is unknown, not absent")
	require.Less(t, time.Since(started), 5*time.Second, "the per-probe deadline, not the discovery helper's own budget, must bound one hung member")
}

// The attack the inconclusive rule exists for. Any member may write any
// authorization server into its own document, so a member that names one it
// does not own becomes the unique match the moment the true owner cannot
// answer — and the resolved URL is where the subject's upstream token goes.
func TestServeConsentAction_GatewayConnectHostileMemberCannotClaimAnAbsentMembersIssuer(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-hostile")

	as := "https://aim87-hostile-as.example.com"
	// The authorization server's true owner, made unable to answer.
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-hostile-owner", newHangingUpstream(t).URL+"/mcp", 0)
	claimant := newGatewayMemberUpstream(t, as)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-hostile-claimant", claimant.URL+"/mcp", 1)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-hostile", as, []uuid.UUID{fx.shared})

	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), 2, "both members must be probe candidates, or this asserts nothing")

	loc := postConnectAction(t, fx, client)
	require.NotEqual(t, claimant.URL+"/mcp", loc.Query().Get("resource"), "the claimant must not receive the subject's upstream token")
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "an unanswered member makes the claimant's match non-unique")
	require.Empty(t, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// Starvation, which is the ambiguity guard's cheapest bypass: rows arrive in
// member-controlled sort_order, so a claimant placed first answers inside the
// first wave while enough silent members behind it exhaust the whole fan-out
// budget before the authorization server's true owner is ever reached. The
// budget must bound latency without ever turning "we ran out of time" into a
// confident match.
func TestServeConsentAction_GatewayConnectBudgetExhaustionSendsNoResource(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-budget")

	as := "https://aim87-budget-as.example.com"
	claimant := newGatewayMemberUpstream(t, as)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-budget-claimant", claimant.URL+"/mcp", 0)
	// Two full waves at the concurrency cap, each bounded by the per-probe
	// deadline, is the whole budget.
	for i := range 8 {
		createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-budget-hang-"+strconv.Itoa(i), newHangingUpstream(t).URL+"/mcp", int32(i+1))
	}
	owner := newGatewayMemberUpstream(t, as)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-budget-owner", owner.URL+"/mcp", 9)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-budget", as, []uuid.UUID{fx.shared})

	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), 10, "every member must be a probe candidate, or this asserts nothing")

	started := time.Now()
	loc := postConnectAction(t, fx, client)
	require.NotEqual(t, claimant.URL+"/mcp", loc.Query().Get("resource"), "the claimant must not win by outlasting the owner's turn")
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a starved fan-out has not established that any claim is unique")
	require.Less(t, time.Since(started), 10*time.Second, "the fan-out budget, not the member count, must bound consent latency")
}

// A shared multi-tenant host answers the origin-style location with its own
// document. Honouring that document's authorization servers matches a member
// on an issuer it never declared, so a document naming some other resource is
// not the member's to speak with.
func TestServeConsentAction_GatewayConnectSkipsDocumentNamingAnotherResource(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-declared")

	as := "https://aim87-declared-as.example.com"
	neighbour := newGatewayMemberUpstreamDeclaring(t, "https://aim87-declared-neighbour.example.com/mcp", as)
	borrowed := createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-declared-borrowed", neighbour.URL+"/mcp", 0)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-declared", as, []uuid.UUID{fx.shared})

	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), 1, "the member must be a probe candidate, or this asserts nothing")

	loc := postConnectAction(t, fx, client)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a document naming another resource does not speak for this member")

	// A member whose document names itself is honoured, spelling differences
	// included — the comparison is the same canonical one the match uses.
	_, err := metamcp_repo.New(fx.ti.conn).DeleteMetaMCPMember(ctx, metamcp_repo.DeleteMetaMCPMemberParams{ID: borrowed.memberID, ProjectID: fx.projectID})
	require.NoError(t, err)
	own := newGatewayMemberUpstreamDeclaringSelf(t, "/mcp/", as)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-declared-own", own.URL+"/mcp", 1)
	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), 1, "the self-declaring member must be the only candidate")

	loc = postConnectAction(t, fx, client)
	require.Equal(t, own.URL+"/mcp", loc.Query().Get("resource"), "a member that names itself keeps its claim")
	require.Equal(t, own.URL+"/mcp", mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// A private member the connecting subject holds no mcp:connect on must not be
// probed, matched, or echoed back in the redirect's resource parameter — the
// same visibility rule the serving path applies to the member list.
func TestServeConsentAction_GatewayConnectExcludesMembersTheSubjectCannotConnectTo(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-rbac")

	as := "https://aim87-rbac-as.example.com"
	private := newGatewayMemberUpstream(t, as)
	member := createGatewayMemberWithVisibility(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-rbac-private", private.URL+"/mcp", 0, "private")

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-rbac", as, []uuid.UUID{fx.shared})

	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), 1, "the member must be a probe candidate, or this asserts nothing")

	loc := postConnectActionAs(t, authztest.WithExactGrants(t, ctx), fx, client)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a member the subject cannot connect to must not claim their credential")

	// The same member, the same probe, the same document: only the grant differs.
	granted := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: authz.NewSelector(authz.ScopeMCPConnect, member.mcpServerID.String()),
	})
	loc = postConnectActionAs(t, granted, fx, client)
	require.Equal(t, private.URL+"/mcp", loc.Query().Get("resource"), "mcp:connect on the member is what makes it selectable")
	require.Equal(t, private.URL+"/mcp", mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
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

// Three members, three authorization servers, three clients: each resolves its
// own member. Two members can be satisfied by a coin flip; three cannot.
func TestServeConsentAction_GatewayConnectResolvesEachOfThreeMembers(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-three")

	type memberCase struct {
		as       string
		upstream string
		clientID uuid.UUID
	}
	cases := make([]memberCase, 0, 3)
	for i, name := range []string{"alpha", "beta", "gamma"} {
		as := "https://aim87-three-" + name + "-as.example.com"
		upstream := newGatewayMemberUpstream(t, as).URL + "/mcp"
		createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-three-"+name, upstream, int32(i))
		cases = append(cases, memberCase{
			as:       as,
			upstream: upstream,
			clientID: createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-three-"+name, as, []uuid.UUID{fx.shared}),
		})
	}

	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), 3)

	for _, tc := range cases {
		loc := postConnectAction(t, fx, tc.clientID)
		require.Equal(t, tc.upstream, loc.Query().Get("resource"), "authorization server %s must resolve its own member", tc.as)
		require.Equal(t, tc.upstream, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
	}
}

// A gateway mixes backend kinds. Only remote members can advertise RFC 9728
// metadata, so only they are probe candidates — and the tunneled, hosted, and
// unproxied members must not contest the match even when their own URL would
// answer for the same authorization server.
func TestServeConsentAction_GatewayConnectExcludesNonRemoteMembersFromTheQuery(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-mixed")

	as := "https://aim87-mixed-as.example.com"
	remote := newGatewayMemberUpstream(t, as)
	// The unproxied member answers for the same authorization server: if it
	// were a candidate the match would be ambiguous and resolve to nothing.
	unproxied := newGatewayMemberUpstream(t, as)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-mixed-remote", remote.URL+"/mcp", 0)
	createTunneledGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-mixed-tunneled", 1)
	createToolsetGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-mixed-hosted", 2)
	createUnproxiedGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-mixed-unproxied", unproxied.URL+"/mcp", 3)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-mixed", as, []uuid.UUID{fx.shared})

	// The exclusion is the query's job, not a filter applied to its rows.
	require.Equal(t, []string{remote.URL + "/mcp"}, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID))

	loc := postConnectAction(t, fx, client)
	require.Equal(t, remote.URL+"/mcp", loc.Query().Get("resource"))
	require.Equal(t, remote.URL+"/mcp", mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// A gateway with no members at all, and a gateway whose only members are
// non-remote, both derive nothing rather than erroring or guessing.
func TestServeConsentAction_GatewayConnectWithNoRemoteMembersSendsNoResource(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-empty")

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-empty", "https://aim87-empty-as.example.com", []uuid.UUID{fx.shared})

	loc := postConnectAction(t, fx, client)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a gateway with no members has nothing to qualify a credential to")

	createTunneledGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-empty-tunneled", 0)
	createToolsetGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-empty-hosted", 1)
	require.Empty(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID))

	loc = postConnectAction(t, fx, client)
	_, hasResource = loc.Query()["resource"]
	require.False(t, hasResource, "members that advertise no metadata document cannot be resolved")
	require.Empty(t, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// Members hidden from the serving path must not participate: each of the three
// exclusions here answers for the live member's authorization server, so any
// one of them leaking into the candidate set makes the match ambiguous and the
// resource empty.
func TestServeConsentAction_GatewayConnectIgnoresDisabledAndDetachedMembers(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-hidden")

	as := "https://aim87-hidden-as.example.com"
	live := newGatewayMemberUpstream(t, as)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-hidden-live", live.URL+"/mcp", 0)

	createGatewayMemberWithVisibility(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-hidden-disabled", newGatewayMemberUpstream(t, as).URL+"/mcp", 1, "disabled")

	tombstoned := createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-hidden-tombstoned", newGatewayMemberUpstream(t, as).URL+"/mcp", 2)
	_, err := mcpservers_repo.New(fx.ti.conn).DeleteMCPServer(ctx, mcpservers_repo.DeleteMCPServerParams{ID: tombstoned.mcpServerID, ProjectID: fx.projectID})
	require.NoError(t, err)

	detached := createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-hidden-detached", newGatewayMemberUpstream(t, as).URL+"/mcp", 3)
	_, err = metamcp_repo.New(fx.ti.conn).DeleteMetaMCPMember(ctx, metamcp_repo.DeleteMetaMCPMemberParams{ID: detached.memberID, ProjectID: fx.projectID})
	require.NoError(t, err)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-hidden", as, []uuid.UUID{fx.shared})

	require.Equal(t, []string{live.URL + "/mcp"}, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID))

	loc := postConnectAction(t, fx, client)
	require.Equal(t, live.URL+"/mcp", loc.Query().Get("resource"), "only the live member may claim the credential")
	require.Equal(t, live.URL+"/mcp", mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// The advertised authorization server is written by the upstream while the
// issuer URL is Gram's stored spelling, so the two disagree on trailing slash,
// default port, and host case for one authorization server. All three must
// match, and the recorded resource is trimmed to exactly the form
// resolveUpstreamResource derives for the backend so routing can compare them.
func TestServeConsentAction_GatewayConnectMatchesEquivalentIssuerSpellings(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-spelling")

	type spellingCase struct {
		name       string
		advertised string
		stored     string
		upstream   string
	}
	cases := []spellingCase{
		{name: "trailing slash on the advertised entry", advertised: "https://aim87-slash-as.example.com/", stored: "https://aim87-slash-as.example.com", upstream: ""},
		{name: "trailing slash on the stored issuer", advertised: "https://aim87-stored-as.example.com", stored: "https://aim87-stored-as.example.com/", upstream: ""},
		{name: "explicit default port", advertised: "https://aim87-port-as.example.com:443", stored: "https://aim87-port-as.example.com", upstream: ""},
		{name: "host case", advertised: "https://AIM87-Case-AS.example.com", stored: "https://aim87-case-as.example.com", upstream: ""},
	}
	clients := make([]uuid.UUID, 0, len(cases))
	for i := range cases {
		// The member's own URL carries a trailing slash the recorded resource
		// must lose, matching resolveUpstreamResource.
		cases[i].upstream = newGatewayMemberUpstream(t, cases[i].advertised).URL + "/mcp/"
		createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-spelling-"+strconv.Itoa(i), cases[i].upstream, int32(i))
		clients = append(clients, createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-spelling-"+strconv.Itoa(i), cases[i].stored, []uuid.UUID{fx.shared}))
	}

	for i, tc := range cases {
		loc := postConnectAction(t, fx, clients[i])
		want := strings.TrimRight(tc.upstream, "/")
		require.Equal(t, want, loc.Query().Get("resource"), "%s must still name one authorization server", tc.name)
		require.Equal(t, want, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
	}
}

// Two members behind one authorization server are unsupported, and a spelling
// difference between their advertised entries must not smuggle one of them
// past that guard.
func TestServeConsentAction_GatewayConnectAmbiguitySurvivesSpellingDifferences(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-spellambig")

	memberA := newGatewayMemberUpstream(t, "https://aim87-spellambig-as.example.com")
	memberB := newGatewayMemberUpstream(t, "https://AIM87-SpellAmbig-AS.example.com:443/")
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-spellambig-a", memberA.URL+"/mcp", 0)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-spellambig-b", memberB.URL+"/mcp", 1)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-spellambig", "https://aim87-spellambig-as.example.com", []uuid.UUID{fx.shared})

	loc := postConnectAction(t, fx, client)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "two spellings of one authorization server are still one authorization server")
	require.Empty(t, mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource)
}

// A different scheme or a different path is a different authorization server,
// so canonical matching must not collapse them onto the connecting client.
func TestServeConsentAction_GatewayConnectKeepsDistinctIssuersApart(t *testing.T) {
	t.Parallel()

	ctx, fx, metaServerID := seedGatewayConsentEndpoint(t, "aim87-gw-distinct")

	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-distinct-scheme", newGatewayMemberUpstream(t, "http://aim87-distinct-as.example.com").URL+"/mcp", 0)
	createGatewayMember(t, ctx, fx.ti.conn, fx.projectID, metaServerID, "aim87-distinct-path", newGatewayMemberUpstream(t, "https://aim87-distinct-as.example.com/TENANT").URL+"/mcp", 1)

	client := createConsentRemoteClient(t, ctx, fx.ti.conn, fx.projectID, fx.orgID, "aim87-distinct", "https://aim87-distinct-as.example.com/tenant", []uuid.UUID{fx.shared})

	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, fx.ti.conn, fx.projectID, metaServerID), 2)

	loc := postConnectAction(t, fx, client)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "scheme and path case distinguish authorization servers")
}

// Precedence, pinned in both directions: on a gateway, consent-time discovery
// answers where it can and the stored per-client derivation answers where it
// cannot. A client bound to both the gateway's issuer and a member's own
// issuer derives through either endpoint, so a discovery miss no longer costs
// it the resource it had — and a discovery hit still overrides what the
// derivation would have said.
func TestServeConsentAction_GatewayConnectFallsBackToStoredDerivation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	projectID, orgID := consentTestTenant(t, ctx)

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	memberIssuer := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, memberIssuer, "aim87-fallback-srv", consentUpstreamA)

	metaServerID := createGatewayMetaServer(t, ctx, ti.conn, projectID, orgID, "aim87-fallback-gw", shared)
	// Live, servable, and simply not advertising OAuth — the "metadata
	// unavailable at consent" case.
	quiet := createGatewayMember(t, ctx, ti.conn, projectID, metaServerID, "aim87-fallback-member", newUpstreamWithoutMetadata(t).URL+"/mcp", 0)

	newFixture := func(slug string) consentActionFixture {
		endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, shared, slug)
		return consentActionFixture{
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
	}

	// Bound to the gateway's issuer and to the member's own issuer, which is
	// exactly the client the stored derivation can still answer for.
	client := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "aim87-fallback", "https://aim87-fallback-as.example.com", []uuid.UUID{shared, memberIssuer})

	direct := newFixture("aim87-fallback-direct")
	require.Equal(t, consentUpstreamA, postConnectAction(t, direct, client).Query().Get("resource"), "the stored derivation does answer for this client")

	require.Len(t, gatewayMemberUpstreamURLs(t, ctx, ti.conn, projectID, metaServerID), 1, "the member must be a probe candidate, or this asserts nothing")

	gateway := newFixture("aim87-fallback-gateway")
	gateway.endpoint.MetaMcpServerID = conv.ToNullUUID(metaServerID)
	loc := postConnectAction(t, gateway, client)
	require.Equal(t, consentUpstreamA, loc.Query().Get("resource"), "a discovery miss on a gateway must not cost the client the resource it derives everywhere else")
	require.Equal(t, consentUpstreamA, mintedRemoteLoginState(t, ctx, gateway, loc.Query().Get("state")).Resource)

	// Swap the silent member for one that claims the client's authorization
	// server: the same client, the same bindings, now resolved by discovery.
	_, err := metamcp_repo.New(ti.conn).DeleteMetaMCPMember(ctx, metamcp_repo.DeleteMetaMCPMemberParams{ID: quiet.memberID, ProjectID: projectID})
	require.NoError(t, err)
	claimed := newGatewayMemberUpstream(t, "https://aim87-fallback-as.example.com")
	createGatewayMember(t, ctx, ti.conn, projectID, metaServerID, "aim87-fallback-claimed", claimed.URL+"/mcp", 1)
	require.Equal(t, []string{claimed.URL + "/mcp"}, gatewayMemberUpstreamURLs(t, ctx, ti.conn, projectID, metaServerID))

	loc = postConnectAction(t, gateway, client)
	require.Equal(t, claimed.URL+"/mcp", loc.Query().Get("resource"), "discovery outranks the stored derivation wherever it resolves")
	require.Equal(t, claimed.URL+"/mcp", mintedRemoteLoginState(t, ctx, gateway, loc.Query().Get("state")).Resource)
}
