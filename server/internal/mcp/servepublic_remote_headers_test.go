package mcp_test

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// protectedInboundHeaders are synthetic values for the Speakeasy credential,
// session, assertion and tunnel transport headers a remote upstream must
// never receive.
var protectedInboundHeaders = map[string]string{
	"Gram-Key":                     "synthetic-api-key",
	"Gram-Session":                 "synthetic-session",
	"Gram-Chat-Session":            "synthetic-chat-session",
	"Gram-Project":                 "synthetic-project",
	"Gram-Consent-State":           "synthetic-consent",
	"X-Gram-Tunnel-Require-Active": "1",
	"X-Gram-Tunnel-Forward-Token":  "synthetic-forward-token",
	"X-Gram-Agent-Version":         "1.0.0",
	"X-Speakeasy-Identity":         "synthetic-assertion",
	"Speakeasy-AI-Key":             "synthetic-ai-key",
	"Speakeasy-AI-Project":         "synthetic-ai-project",
}

// headerRecordingUpstream is an MCP upstream that records the names of the
// headers on each MCP request it receives, never their values, apart from
// equality markers the test asks it to keep.
type headerRecordingUpstream struct {
	mu      sync.Mutex
	names   []map[string]struct{}
	markers []map[string]string
	server  *httptest.Server
}

func newHeaderRecordingUpstream(t *testing.T, markerNames ...string) *headerRecordingUpstream {
	t.Helper()

	u := &headerRecordingUpstream{}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The proxy's background metadata probe is not MCP traffic.
		if r.URL.Path == wellknown.OAuthProtectedResourcePath {
			http.NotFound(w, r)
			return
		}
		names := make(map[string]struct{}, len(r.Header))
		for name := range r.Header {
			names[http.CanonicalHeaderKey(name)] = struct{}{}
		}
		markers := make(map[string]string, len(markerNames))
		for _, name := range markerNames {
			markers[name] = r.Header.Get(name)
		}
		u.mu.Lock()
		u.names = append(u.names, names)
		u.markers = append(u.markers, markers)
		u.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"upstream","version":"1.0"}}}`))
	}))
	t.Cleanup(u.server.Close)
	return u
}

func (u *headerRecordingUpstream) requests() ([]map[string]struct{}, []map[string]string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]map[string]struct{}(nil), u.names...), append([]map[string]string(nil), u.markers...)
}

func seedRemoteHeader(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, remoteServerID uuid.UUID, name string, value string, source string, required bool) {
	t.Helper()

	remotemcptest.SeedHeader(t, ctx, conn, remotemcprepo.CreateServerHeaderParams{
		RemoteMcpServerID:      remoteServerID,
		ProjectID:              projectID,
		Name:                   name,
		Description:            conv.PtrToPGText(nil),
		IsRequired:             required,
		IsSecret:               false,
		Value:                  conv.PtrToPGTextEmpty(&value),
		ValueFromRequestHeader: conv.PtrToPGTextEmpty(&source),
	})
}

func TestServePublic_RemoteEndpoint_DoesNotForwardSpeakeasyHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	upstream := newHeaderRecordingUpstream(t, "X-Api-Key", "X-Client-Trace")
	endpointSlug := "endpoint-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	mcpServer, remoteServer := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.server.URL, endpointSlug, "public", issuerID)
	// A static credential stored before names were canonicalized still works;
	// an optional row reading a Speakeasy credential is not sent.
	seedRemoteHeader(t, ctx, ti.conn, *authCtx.ProjectID, remoteServer.ID, "x-api-key", "operator-credential", "", true)
	seedRemoteHeader(t, ctx, ti.conn, *authCtx.ProjectID, remoteServer.ID, "X-Upstream-Token", "", "Gram-Key", false)
	token := mintIssuerBearerForEndpoint(t, ctx, ti, endpointSlug, mcpServer, authCtx.ActiveOrganizationID)

	extra := map[string]string{"X-Client-Trace": "allowed"}
	maps.Copy(extra, protectedInboundHeaders)
	// The caller's own copy under the suppressed row's name must not stand in.
	extra["X-Upstream-Token"] = "client-supplied"

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), token, extra)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	names, markers := upstream.requests()
	require.NotEmpty(t, names, "the request must reach the upstream for absence to mean anything")
	for i, received := range names {
		for name := range protectedInboundHeaders {
			require.NotContains(t, received, http.CanonicalHeaderKey(name), "%s reached the remote upstream", name)
		}
		require.NotContains(t, received, "Authorization", "the caller's Speakeasy bearer reached the remote upstream")
		require.NotContains(t, received, "X-Upstream-Token")
		require.Equal(t, "allowed", markers[i]["X-Client-Trace"])
		require.Equal(t, "operator-credential", markers[i]["X-Api-Key"])
	}
}

func TestServePublic_RemoteEndpoint_RequiredProtectedSourceRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	upstream := newHeaderRecordingUpstream(t)
	endpointSlug := "endpoint-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	mcpServer, remoteServer := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.server.URL, endpointSlug, "public", issuerID)
	seedRemoteHeader(t, ctx, ti.conn, *authCtx.ProjectID, remoteServer.ID, "X-Upstream-Token", "", "gram-chat-session", true)
	token := mintIssuerBearerForEndpoint(t, ctx, ti, endpointSlug, mcpServer, authCtx.ActiveOrganizationID)

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeInitializeBody(), token, map[string]string{
		"Gram-Chat-Session": "synthetic-chat-session",
	})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable, "status=%d body=%s", w.Code, w.Body.String())
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
	require.Contains(t, shareable.Error(), `"X-Upstream-Token"`)
	require.NotContains(t, shareable.Error(), "synthetic-chat-session")

	names, _ := upstream.requests()
	require.Empty(t, names, "the upstream must not be called")
}

// The consent page's live MCP transport proxies the browser's request to the
// remote upstream; Speakeasy headers on it are dropped there as well.
func TestServeConsentMCP_RemoteDoesNotForwardSpeakeasyHeaders(t *testing.T) {
	t.Parallel()

	upstream := newHeaderRecordingUpstream(t, "X-Api-Key", "X-Client-Trace")
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	endpointSlug := "remote-consent-headers-" + uuid.NewString()
	mcpServer, remoteServer := createRemoteMcpEndpoint(t, ctx, ti.conn, issuer.ProjectID.UUID, upstream.server.URL, endpointSlug, "public", issuer.ID)
	seedRemoteHeader(t, ctx, ti.conn, issuer.ProjectID.UUID, remoteServer.ID, "x-api-key", "operator-credential", "", true)
	seedRemoteHeader(t, ctx, ti.conn, issuer.ProjectID.UUID, remoteServer.ID, "X-Upstream-Token", "", "Gram-Key", false)
	stateID, csrfToken := seedModernConsentChallenge(t, ctx, ti, issuer.ID, client, mcpServer.ID, endpointSlug)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, endpointSlug, "x/mcp")
	require.NoError(t, err)

	extra := map[string]string{"X-Client-Trace": "allowed", "X-Upstream-Token": "client-supplied"}
	for name, value := range protectedInboundHeaders {
		// The helper sends the real consent transport headers itself.
		if !strings.HasPrefix(name, "Gram-Consent-") {
			extra[name] = value
		}
	}
	// A synthetic consent field the transport does not consume itself.
	extra["Gram-Consent-Extra"] = "synthetic-consent"
	initializeBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"consent-test","version":"1.0.0"}}}`
	serveConsentMCPRequest(t, ctx, ti, endpoint, stateID, csrfToken, uuid.NewString(), initializeBody, extra)

	names, markers := upstream.requests()
	require.NotEmpty(t, names, "the request must reach the upstream for absence to mean anything")
	for i, received := range names {
		for name := range protectedInboundHeaders {
			require.NotContains(t, received, http.CanonicalHeaderKey(name), "%s reached the remote upstream", name)
		}
		for name := range received {
			require.NotContains(t, strings.ToLower(name), "gram-consent", "consent transport header %s reached the remote upstream", name)
		}
		require.NotContains(t, received, "X-Upstream-Token")
		require.Equal(t, "allowed", markers[i]["X-Client-Trace"])
		require.Equal(t, "operator-credential", markers[i]["X-Api-Key"])
	}
}

// remoteServerIDOf returns the remote_mcp_servers row behind an mcp_servers row.
func remoteServerIDOf(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, mcpServerID uuid.UUID) uuid.UUID {
	t.Helper()

	server, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        mcpServerID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.True(t, server.RemoteMcpServerID.Valid)
	return server.RemoteMcpServerID.UUID
}

// A meta MCP dials each remote member through the hardened remote header
// policy: a credential stored under a lowercase name reaches the member, a
// Set-Cookie row does not, and a required row reading a Speakeasy credential
// fails the call before the member is contacted.
func TestServePublic_MetaEndpoint_RemoteMembersApplyRemoteHeaderPolicy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-header-policy-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	upstreamA := newRecordingUpstream(t, "ping")
	upstreamB := newRecordingUpstream(t, "ping")
	memberA := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member A", "member-a", 0, upstreamA.url)
	memberB := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member B", "member-b", 1, upstreamB.url)
	remoteA := remoteServerIDOf(t, ctx, ti.conn, projectID, memberA)
	remoteB := remoteServerIDOf(t, ctx, ti.conn, projectID, memberB)
	seedRemoteHeader(t, ctx, ti.conn, projectID, remoteA, "x-api-key", "operator-credential", "", true)
	seedRemoteHeader(t, ctx, ti.conn, projectID, remoteA, "Set-Cookie", "synthetic=1", "", false)
	seedRemoteHeader(t, ctx, ti.conn, projectID, remoteB, "X-Upstream-Token", "", "Authorization", true)

	clientA := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-header-a", "", []uuid.UUID{sharedIssuerID})
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-header-b", "", []uuid.UUID{sharedIssuerID})
	subject := createTestUser(t, ctx, ti, "meta-header-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientA, subject, "token-member-a", upstreamA.url)
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientB, subject, "token-member-b", upstreamB.url)
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-a--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "member A execute_tool must succeed: %s", text)
	var sawToolCall bool
	for _, req := range upstreamA.journal() {
		if req.httpMethod != http.MethodPost {
			continue
		}
		sawToolCall = sawToolCall || req.rpcMethod == "tools/call"
		require.Equal(t, "operator-credential", req.apiKey, "rpc %s", req.rpcMethod)
		require.Empty(t, req.setCookie, "rpc %s", req.rpcMethod)
	}
	require.True(t, sawToolCall)

	rpc = executeMetaTool(t, ti, metaSlug, bearer, "member-b--ping")
	_, isError = metaToolResultText(t, rpc)
	require.True(t, isError, "a required row reading Authorization must fail member B's call")
	for _, req := range upstreamB.journal() {
		// Only the proxy's background metadata probe may reach member B.
		require.NotEqual(t, http.MethodPost, req.httpMethod, "member B must receive no MCP request, got %q", req.rpcMethod)
	}
}

// tunnelHeaderPolicyFiles are the only files allowed to select the tunneled
// header policy: the two places that build a proxy to a tunnel gateway.
var tunnelHeaderPolicyFiles = []string{"tunnel_manager.go", "tunnelpublic.go"}

// A proxy built without a policy gets the hardened remote one, so the way to
// leave a remote path unprotected is to select the tunneled policy for it.
// Fail if any other file in this package starts doing so.
func TestOnlyTunnelBuildersSelectTunneledHeaderPolicy(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	var selecting []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		require.NoError(t, err)
		if strings.Contains(string(source), "HeaderPolicyTunneled") {
			selecting = append(selecting, file)
		}
	}
	slices.Sort(selecting)
	require.Equal(t, tunnelHeaderPolicyFiles, selecting)
}
