// The meta MCP's whole credential claim, asserted at the upstream: an
// execute_tool through a meta MCP forwards the member's own bearer to the
// member's own upstream and never a sibling's. Each member here is a real
// MCP SDK server behind a recording reverse proxy, so the assertion reads
// the Authorization header that actually arrived on the wire — and the SDK
// server requires a session, so the inline initialize handshake is exercised
// on every call.
package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// metadataProbeWait bounds how long a test waits for the proxy's detached
// protected resource probe to reach a recorder. The probe's own discovery
// budget is ten seconds, so a probe still missing after this never started.
const metadataProbeWait = 15 * time.Second

// recordedRequest is one request a recordingUpstream received.
type recordedRequest struct {
	// httpMethod is the request's HTTP method.
	httpMethod string

	// path is the request's URL path.
	path string

	// rpcMethod is the JSON-RPC method of a POST body, empty otherwise.
	rpcMethod string

	// authorization is the Authorization header as it arrived, empty when absent.
	authorization string
}

// recordingUpstream is one member's live upstream plus every request that
// reached it, in arrival order. Credential assertions read the tools/call
// requests specifically: the same upstream also receives the session
// lifecycle and the proxy's unauthenticated protected resource probe.
type recordingUpstream struct {
	url string

	// holdMetadataProbe makes the recorder hold protected resource metadata
	// requests until a tools/call response has completed, forcing the probe
	// to arrive after the credentialed call.
	holdMetadataProbe bool

	// toolCallServed closes once the first tools/call response has completed.
	toolCallServed     chan struct{}
	toolCallServedOnce sync.Once

	// metadataProbeServed closes once the first protected resource metadata
	// request has been proxied.
	metadataProbeServed     chan struct{}
	metadataProbeServedOnce sync.Once

	mu       sync.Mutex
	requests []recordedRequest
}

// recordingUpstreamOption configures a recordingUpstream before it serves.
type recordingUpstreamOption func(*recordingUpstream)

// withMetadataProbeHeldUntilToolCall holds the proxy's protected resource
// probe until a tools/call has completed, the interleaving in which an
// unauthenticated probe is the last request the upstream sees.
func withMetadataProbeHeldUntilToolCall() recordingUpstreamOption {
	return func(u *recordingUpstream) { u.holdMetadataProbe = true }
}

// newRecordingUpstream stands up a real MCP SDK server behind a reverse
// proxy that records every request it forwards.
func newRecordingUpstream(t *testing.T, toolName string, opts ...recordingUpstreamOption) *recordingUpstream {
	t.Helper()

	mock := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, []testmcp.Tool{{
		Name:        toolName,
		Description: "returns pong",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Response: testmcp.ToolResponse{
			Content: []map[string]any{{"type": "text", "text": "pong from " + toolName}},
		},
	}})
	t.Cleanup(mock.Close)

	target, err := url.Parse(mock.URL)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)

	u := &recordingUpstream{
		url:                     "",
		holdMetadataProbe:       false,
		toolCallServed:          make(chan struct{}),
		toolCallServedOnce:      sync.Once{},
		metadataProbeServed:     make(chan struct{}),
		metadataProbeServedOnce: sync.Once{},
		mu:                      sync.Mutex{},
		requests:                nil,
	}
	for _, opt := range opts {
		opt(u)
	}

	recorder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isMetadataProbe := r.Method == http.MethodGet && r.URL.Path == wellknown.OAuthProtectedResourcePath
		if isMetadataProbe && u.holdMetadataProbe {
			select {
			case <-u.toolCallServed:
			case <-r.Context().Done():
				return
			}
		}

		rpcMethod, err := u.record(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		proxy.ServeHTTP(w, r)

		switch {
		case rpcMethod == mcpversions.MethodToolsCall:
			u.toolCallServedOnce.Do(func() { close(u.toolCallServed) })
		case isMetadataProbe:
			u.metadataProbeServedOnce.Do(func() { close(u.metadataProbeServed) })
		}
	}))
	t.Cleanup(recorder.Close)
	// Registered after recorder.Close so it runs first: Close waits for a
	// held probe's handler to return.
	t.Cleanup(func() { u.toolCallServedOnce.Do(func() { close(u.toolCallServed) }) })

	u.url = recorder.URL
	return u
}

// record appends r to the journal and returns its JSON-RPC method, leaving
// the body readable for the proxy.
func (u *recordingUpstream) record(r *http.Request) (string, error) {
	var rpcMethod string
	if r.Method == http.MethodPost {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return "", fmt.Errorf("read upstream request body: %w", err)
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))

		var rpc struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(raw, &rpc) == nil {
			rpcMethod = rpc.Method
		}
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	u.requests = append(u.requests, recordedRequest{
		httpMethod:    r.Method,
		path:          r.URL.Path,
		rpcMethod:     rpcMethod,
		authorization: r.Header.Get("Authorization"),
	})
	return rpcMethod, nil
}

func (u *recordingUpstream) journal() []recordedRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.requests)
}

func (u *recordingUpstream) requestCount() int {
	return len(u.journal())
}

// requireToolCallAuth asserts the upstream received at least one tools/call
// and that every tools/call carried exactly want; an empty want asserts an
// anonymous call.
func (u *recordingUpstream) requireToolCallAuth(t *testing.T, want string, msgAndArgs ...any) {
	t.Helper()

	var got []string
	for _, req := range u.journal() {
		if req.rpcMethod == mcpversions.MethodToolsCall {
			got = append(got, req.authorization)
		}
	}
	require.NotEmpty(t, got, "the upstream must have received a tools/call")
	for _, auth := range got {
		require.Equal(t, want, auth, msgAndArgs...)
	}
}

// requireNeverCarried asserts no request of any kind reached the upstream
// with the given Authorization value.
func (u *recordingUpstream) requireNeverCarried(t *testing.T, authorization string, msgAndArgs ...any) {
	t.Helper()

	for _, req := range u.journal() {
		require.NotEqual(t, authorization, req.authorization, msgAndArgs...)
	}
}

// awaitMetadataProbe waits for the proxy's detached protected resource probe
// to have been served by the upstream.
func (u *recordingUpstream) awaitMetadataProbe(t *testing.T) {
	t.Helper()

	select {
	case <-u.metadataProbeServed:
	case <-time.After(metadataProbeWait):
		require.FailNow(t, "the proxy never probed the upstream's protected resource metadata")
	}
}

// seedMetaMemberWithUpstream is seedMetaMember pointed at a live upstream,
// returning the created member server id.
func seedMetaMemberWithUpstream(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	projectID uuid.UUID,
	metaID uuid.UUID,
	name, slug string,
	sortOrder int32,
	upstreamURL string,
) uuid.UUID {
	t.Helper()

	remote := remotemcptest.SeedServer(t, ctx, conn, remotemcprepo.CreateServerParams{
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           upstreamURL,
	})
	memberIssuerID := createUserSessionIssuer(t, ctx, conn, projectID)

	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  serverID,
		ProjectID:           projectID,
		Name:                conv.ToPGText(name),
		Slug:                conv.ToPGText(slug),
		EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID: uuid.NullUUID{UUID: memberIssuerID, Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remote.ID, Valid: true},
		ToolsetID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:          "public",
	})
	require.NoError(t, err)

	_, err = metamcprepo.New(conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID:       projectID,
		MetaMcpServerID: metaID,
		McpServerID:     server.ID,
		SortOrder:       sortOrder,
	})
	require.NoError(t, err)
	return server.ID
}

// insertQualifiedRemoteSessionToken plants a stored upstream token carrying
// an RFC 8707 resource, the qualified form the meta MCP's strict router
// selects by.
func insertQualifiedRemoteSessionToken(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	userSessionIssuerID uuid.UUID,
	remoteSessionClientID uuid.UUID,
	subject urn.SessionSubject,
	accessToken string,
	resource string,
) {
	t.Helper()

	accessTokenEncrypted, err := ti.enc.Encrypt([]byte(accessToken))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userSessionIssuerID,
		RemoteSessionClientID: remoteSessionClientID,
		AccessTokenEncrypted:  accessTokenEncrypted,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		RefreshTokenEncrypted: pgtype.Text{String: "", Valid: false},
		RefreshExpiresAt:      pgtype.Timestamptz{Valid: false},
		Scopes:                []string{},
		Resource:              pgtype.Text{String: resource, Valid: resource != ""},
	})
	require.NoError(t, err)
}

// mintMetaIssuerBearer mints and persists a user-session bearer for an
// issuer-gated meta endpoint.
func mintMetaIssuerBearer(t *testing.T, ti *testInstance, metaSlug string, issuerID uuid.UUID, subject urn.SessionSubject) string {
	t.Helper()

	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  subject,
		Audience: urn.NewUserSessionIssuer(issuerID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + metaSlug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, subject, jti)
	return token
}

// executeMetaTool drives execute_tool through the public HTTP surface and
// returns the decoded tool result.
func executeMetaTool(t *testing.T, ti *testInstance, metaSlug, bearer, qualified string) map[string]json.RawMessage {
	t.Helper()

	body := makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "execute_tool",
		"arguments": map[string]any{"name": qualified, "arguments": map[string]any{}},
	})
	resp, err := servePublicHTTP(t, context.Background(), ti, metaSlug, body, bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, "execute_tool response: %s", resp.Body.String())
	return decodeRPCResponse(t, resp)
}

func metaToolResultText(t *testing.T, rpc map[string]json.RawMessage) (text string, isError bool) {
	t.Helper()
	var res struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NotNil(t, rpc["result"], "rpc response has no result: %v", rpc)
	require.NoError(t, json.Unmarshal(rpc["result"], &res))
	parts := make([]string, 0, len(res.Content))
	for _, chunk := range res.Content {
		if chunk.Text != "" {
			parts = append(parts, chunk.Text)
		}
	}
	return strings.Join(parts, "\n"), res.IsError
}

// TestServePublic_MetaEndpoint_ExecuteTool_ForwardsEachMembersOwnBearer is
// the AIM-87 end-to-end credential acceptance test.
func TestServePublic_MetaEndpoint_ExecuteTool_ForwardsEachMembersOwnBearer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-cred-e2e-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	upstreamA := newRecordingUpstream(t, "ping")
	upstreamB := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member A", "member-a", 0, upstreamA.url)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member B", "member-b", 1, upstreamB.url)

	// One client per upstream authorization server, both bound to the
	// meta MCP's shared issuer — the shape meta MCP consent produces.
	clientA := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-cred-a", "", []uuid.UUID{sharedIssuerID})
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-cred-b", "", []uuid.UUID{sharedIssuerID})

	subject := createTestUser(t, ctx, ti, "meta-cred-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientA, subject, "token-member-a", upstreamA.url)
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientB, subject, "token-member-b", upstreamB.url)

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-a--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "member A execute_tool must succeed: %s", text)
	require.Contains(t, text, "pong from ping")
	upstreamA.requireToolCallAuth(t, "Bearer token-member-a",
		"member A's upstream must receive exactly member A's bearer")
	require.Zero(t, upstreamB.requestCount(), "member B's upstream must not be contacted by member A's call")

	rpc = executeMetaTool(t, ti, metaSlug, bearer, "member-b--ping")
	_, isError = metaToolResultText(t, rpc)
	require.False(t, isError, "member B execute_tool must succeed")
	upstreamB.requireToolCallAuth(t, "Bearer token-member-b",
		"member B's upstream must receive exactly member B's bearer and never A's")
	upstreamA.requireNeverCarried(t, "Bearer token-member-b", "member A's upstream must never see member B's bearer")
	upstreamB.requireNeverCarried(t, "Bearer token-member-a", "member B's upstream must never see member A's bearer")

	// The proxied members' tool_call rows carry the gateway id.
	requireTelemetryRowCount(t, `meta_mcp_server_id = ? AND event_source = 'tool_call' AND tool_name = 'ping'`, 2, meta.ID.String())
}

// A caller's _meta must not reach a proxied member: WireMeta re-serializes
// lossily (empty/null fields) and strict vendors reject the result with 400.
// Regression for a real-client failure — MCP clients attach _meta routinely.
func TestServePublic_MetaEndpoint_ExecuteTool_DropsCallerMetaUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-nometa-e2e-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	upstream := newRecordingUpstream(t, "ping")
	var bodies sync.Map
	var bodyIdx atomic.Int64
	target, perr := url.Parse(upstream.url)
	require.NoError(t, perr)
	reverse := httputil.NewSingleHostReverseProxy(target)
	recorder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, rerr := io.ReadAll(r.Body)
		if rerr != nil {
			http.Error(w, rerr.Error(), http.StatusInternalServerError)
			return
		}
		bodies.Store(bodyIdx.Add(1), string(raw))
		r.Body = io.NopCloser(bytes.NewReader(raw))
		reverse.ServeHTTP(w, r)
	}))
	t.Cleanup(recorder.Close)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member", "member-nometa", 0, recorder.URL)

	subject := createTestUser(t, ctx, ti, "meta-nometa-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	body := makeMetaRPCBody(t, "tools/call", map[string]any{
		"_meta":     map[string]any{"progressToken": 1, "claudecode/toolUseId": "toolu_regression"},
		"name":      "execute_tool",
		"arguments": map[string]any{"name": "member-nometa--ping", "arguments": map[string]any{}},
	})
	resp, err := servePublicHTTP(t, context.Background(), ti, metaSlug, body, bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, "execute_tool response: %s", resp.Body.String())
	text, isError := metaToolResultText(t, decodeRPCResponse(t, resp))
	require.False(t, isError, "execute_tool with caller _meta must succeed: %s", text)
	require.Contains(t, text, "pong from ping")

	seen := 0
	bodies.Range(func(_, v any) bool {
		seen++
		body, isString := v.(string)
		require.True(t, isString)
		require.NotContains(t, body, "claudecode/toolUseId", "caller metadata must not leak")
		require.NotContains(t, body, "toolu_regression", "caller metadata must not leak")
		require.NotContains(t, body, "progressToken", "caller progress token must not leak")
		return true
	})
	require.Positive(t, seen, "the upstream recorder must have observed requests")
}

// An ambiguous credential map — two stored tokens claiming one member's
// resource — must fail member-scoped with the upstream never contacted.
func TestServePublic_MetaEndpoint_ExecuteTool_AmbiguousCredentialMakesNoCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-ambig-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	upstream := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member", "member", 0, upstream.url)

	clientA := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-ambig-a", "", []uuid.UUID{sharedIssuerID})
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-ambig-b", "", []uuid.UUID{sharedIssuerID})

	subject := createTestUser(t, ctx, ti, "meta-ambig-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientA, subject, "token-one", upstream.url)
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientB, subject, "token-two", upstream.url)

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member--ping")
	text, isError := metaToolResultText(t, rpc)
	require.True(t, isError, "an ambiguous credential map must fail the member call")
	require.Contains(t, text, "recorded for the same upstream", "the message must name the duplication, not just report misconfiguration")
	require.Zero(t, upstream.requestCount(), "no call may be made when the credential is ambiguous")
}

// A member with no matching credential is called anonymously — no bearer at
// all, and never a sibling's.
func TestServePublic_MetaEndpoint_ExecuteTool_NoCredentialCallsAnonymously(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-anon-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	upstreamA := newRecordingUpstream(t, "ping")
	upstreamB := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member A", "member-a", 0, upstreamA.url)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member B", "member-b", 1, upstreamB.url)

	// Two tokens exist so no lone-entry shortcut could ever apply, but only
	// member B's resource is claimed; member A matches nothing.
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-anon-b", "", []uuid.UUID{sharedIssuerID})
	clientC := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-anon-c", "", []uuid.UUID{sharedIssuerID})

	subject := createTestUser(t, ctx, ti, "meta-anon-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientB, subject, "token-member-b", upstreamB.url)
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientC, subject, "token-elsewhere", "https://elsewhere.example.com/mcp")

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-a--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "an uncredentialed member must still be callable anonymously: %s", text)
	upstreamA.requireToolCallAuth(t, "",
		"no bearer may be forwarded to a member with no matching credential — least of all a sibling's")
	upstreamA.requireNeverCarried(t, "Bearer token-member-b", "member A's upstream must never see member B's bearer")
}

// A subject who connected only one of the gateway's two providers still gets
// a served session: the covered member forwards its own bearer, the
// uncovered member is called anonymously, and no request-level 401 fires.
// Before partial resolution, the missing grant rejected the whole session.
func TestServePublic_MetaEndpoint_PartialProviderConnectionsServe(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-partial-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	upstreamA := newRecordingUpstream(t, "ping")
	upstreamB := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member A", "member-a", 0, upstreamA.url)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member B", "member-b", 1, upstreamB.url)

	// Both providers are attached to the gateway, but the subject linked
	// only member B's — the exact shape of a user who skipped one consent
	// tile. Member A's client stays attached with no token on purpose: the
	// partial resolver must skip it, where the strict resolver would have
	// failed the whole session.
	createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-partial-a", "", []uuid.UUID{sharedIssuerID})
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-partial-b", "", []uuid.UUID{sharedIssuerID})

	subject := createTestUser(t, ctx, ti, "meta-partial-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientB, subject, "token-member-b", upstreamB.url)

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-b--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "the covered member must serve on a partially connected session: %s", text)
	upstreamB.requireToolCallAuth(t, "Bearer token-member-b",
		"the covered member must receive its own bearer")

	rpc = executeMetaTool(t, ti, metaSlug, bearer, "member-a--ping")
	text, isError = metaToolResultText(t, rpc)
	require.False(t, isError, "the uncovered member must degrade to an anonymous call, not fail the session: %s", text)
	upstreamA.requireToolCallAuth(t, "",
		"no bearer may reach the member whose provider was never connected")
	upstreamA.requireNeverCarried(t, "Bearer token-member-b", "member A's upstream must never see member B's bearer")
}

// The proxy probes a member's protected resource metadata off the request
// path, without a bearer. A probe landing after the credentialed tools/call
// must not change what the call is asserted to have carried.
func TestServePublic_MetaEndpoint_ExecuteTool_MetadataProbeAfterToolCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-probe-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	upstream := newRecordingUpstream(t, "ping", withMetadataProbeHeldUntilToolCall())
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member", "member-probe", 0, upstream.url)

	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-probe", "", []uuid.UUID{sharedIssuerID})
	subject := createTestUser(t, ctx, ti, "meta-probe-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientID, subject, "token-member", upstream.url)
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-probe--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "execute_tool must succeed: %s", text)
	upstream.awaitMetadataProbe(t)

	journal := upstream.journal()
	toolCallIdx := slices.IndexFunc(journal, func(req recordedRequest) bool {
		return req.rpcMethod == mcpversions.MethodToolsCall
	})
	probeIdx := slices.IndexFunc(journal, func(req recordedRequest) bool {
		return req.httpMethod == http.MethodGet && req.path == wellknown.OAuthProtectedResourcePath
	})
	require.NotEqual(t, -1, toolCallIdx, "the upstream must have received a tools/call")
	require.Greater(t, probeIdx, toolCallIdx, "the metadata probe must arrive after the tools/call")
	require.Empty(t, journal[probeIdx].authorization, "the metadata probe carries no bearer")
	upstream.requireToolCallAuth(t, "Bearer token-member",
		"a later unauthenticated probe must not change what the tools/call carried")
}

// An upstream 401 means two different things to the caller: the gateway
// found no credential that routes to the member, or it forwarded one the
// member refused. The message must say which, since only the second is fixed
// by a reconnect.
func TestServePublic_MetaEndpoint_ExecuteTool_UnauthorizedNamesAnonymousDial(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-401-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(deny.Close)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, meta.ID, "Member", "member", 0, deny.URL)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-401", "", []uuid.UUID{sharedIssuerID})
	subject := createTestUser(t, ctx, ti, "meta-401-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	// A legacy grant with no resource routes nowhere: the dial is anonymous.
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientID, subject, "token-legacy", "")
	text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.True(t, isError)
	require.Contains(t, text, "holds no credential that routes to it")

	// A grant qualified to the member is forwarded, and the member refuses it.
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientID, subject, "token-refused", deny.URL)
	text, isError = metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.True(t, isError)
	require.Contains(t, text, "rejected the stored credential")
}
