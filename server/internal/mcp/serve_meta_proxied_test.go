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
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
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
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// recordingUpstream is one member's live upstream plus what reached it.
type recordingUpstream struct {
	url      string
	auth     *atomic.Value
	requests *atomic.Int64
}

func (u *recordingUpstream) capturedAuth() string {
	got, _ := u.auth.Load().(string)
	return got
}

// newRecordingUpstream stands up a real MCP SDK server behind a reverse
// proxy that records the Authorization header and request count.
func newRecordingUpstream(t *testing.T, toolName string) *recordingUpstream {
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

	captured := &atomic.Value{}
	captured.Store("")
	requests := &atomic.Int64{}
	recorder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		captured.Store(r.Header.Get("Authorization"))
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(recorder.Close)

	return &recordingUpstream{url: recorder.URL, auth: captured, requests: requests}
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

	subject := urn.NewUserSubject("meta-cred-user-" + uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientA, subject, "token-member-a", upstreamA.url)
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientB, subject, "token-member-b", upstreamB.url)

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-a--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "member A execute_tool must succeed: %s", text)
	require.Contains(t, text, "pong from ping")
	require.Equal(t, "Bearer token-member-a", upstreamA.capturedAuth(),
		"member A's upstream must receive exactly member A's bearer")
	require.Zero(t, upstreamB.requests.Load(), "member B's upstream must not be contacted by member A's call")

	rpc = executeMetaTool(t, ti, metaSlug, bearer, "member-b--ping")
	_, isError = metaToolResultText(t, rpc)
	require.False(t, isError, "member B execute_tool must succeed")
	require.Equal(t, "Bearer token-member-b", upstreamB.capturedAuth(),
		"member B's upstream must receive exactly member B's bearer and never A's")
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

	subject := urn.NewUserSubject("meta-nometa-user-" + uuid.NewString())
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
		require.NotContains(t, body, `"_meta"`,
			"no upstream request may carry a _meta object")
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

	subject := urn.NewUserSubject("meta-ambig-user-" + uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientA, subject, "token-one", upstream.url)
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientB, subject, "token-two", upstream.url)

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member--ping")
	text, isError := metaToolResultText(t, rpc)
	require.True(t, isError, "an ambiguous credential map must fail the member call")
	require.Contains(t, text, "recorded for the same upstream", "the message must name the duplication, not just report misconfiguration")
	require.Zero(t, upstream.requests.Load(), "no call may be made when the credential is ambiguous")
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

	subject := urn.NewUserSubject("meta-anon-user-" + uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientB, subject, "token-member-b", upstreamB.url)
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientC, subject, "token-elsewhere", "https://elsewhere.example.com/mcp")

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-a--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "an uncredentialed member must still be callable anonymously: %s", text)
	require.Positive(t, upstreamA.requests.Load(), "member A's upstream must have been called")
	require.Empty(t, upstreamA.capturedAuth(),
		"no bearer may be forwarded to a member with no matching credential — least of all a sibling's")
}
