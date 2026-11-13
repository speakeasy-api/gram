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
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
)

// keyRefusingUpstream refuses with status and challenge, recording the headers of each refused MCP request.
type keyRefusingUpstream struct {
	*httptest.Server

	mu    sync.Mutex
	seen  []http.Header
	other *httptest.Server
}

// upstreamRefusal is when the upstream refuses: every request, only tools/call after a live session, or after a redirect to another origin.
type upstreamRefusal int

const (
	refuseAll upstreamRefusal = iota
	refuseToolsCall
	refuseAfterRedirect
)

func newKeyRefusingUpstream(t *testing.T, status int, challenge string, when upstreamRefusal) *keyRefusingUpstream {
	t.Helper()
	u := &keyRefusingUpstream{Server: nil, mu: sync.Mutex{}, seen: nil, other: nil}
	refuse := func(w http.ResponseWriter, r *http.Request) {
		// A 302 turns the redirected POST into a GET.
		if r.Method == http.MethodPost || when == refuseAfterRedirect {
			u.mu.Lock()
			u.seen = append(u.seen, r.Header.Clone())
			u.mu.Unlock()
		}
		if challenge != "" {
			w.Header().Set("WWW-Authenticate", challenge)
		}
		w.WriteHeader(status)
	}
	var serve http.HandlerFunc = refuse
	switch when {
	case refuseAll:
	case refuseToolsCall:
		mock := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: []testmcp.Tool{{
			Name:        "ping",
			Description: "returns pong",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			Response:    testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "pong"}}},
		}}})
		t.Cleanup(mock.Close)
		target, err := url.Parse(mock.URL)
		require.NoError(t, err)
		live := httputil.NewSingleHostReverseProxy(target)
		serve = func(w http.ResponseWriter, r *http.Request) {
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var rpc struct {
				Method string `json:"method"`
			}
			if json.Unmarshal(raw, &rpc) == nil && rpc.Method == mcpversions.MethodToolsCall {
				refuse(w, r)
				return
			}
			live.ServeHTTP(w, r)
		}
	case refuseAfterRedirect:
		u.other = httptest.NewServer(http.HandlerFunc(refuse))
		t.Cleanup(u.other.Close)
		serve = func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, u.other.URL, http.StatusFound)
		}
	}
	u.Server = httptest.NewServer(serve)
	t.Cleanup(u.Close)
	return u
}

// requireRefused asserts the refused requests carried key, or none when empty, and never an Authorization header.
func (u *keyRefusingUpstream) requireRefused(t *testing.T, key string) {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	require.NotEmpty(t, u.seen, "the upstream must have refused a request")
	for _, header := range u.seen {
		require.Equal(t, key, header.Get("X-Api-Key"))
		require.Empty(t, header.Get("Authorization"), "an anonymous call carries no token")
	}
}

// createKeyMember is createMetaMember over streamable HTTP, returning the member server and its remote server.
func createKeyMember(t *testing.T, ctx context.Context, ti *testInstance, projectID, metaID uuid.UUID, upstreamURL string, remoteIssuerID uuid.NullUUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	remote := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcprepo.CreateServerParams{ProjectID: projectID, TransportType: "streamable-http", Url: upstreamURL})
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText("member"),
		Slug:                conv.ToPGText("member"),
		EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID: conv.ToNullUUID(createUserSessionIssuer(t, ctx, ti.conn, projectID)),
		RemoteMcpServerID:   conv.ToNullUUID(remote.ID),
		ToolsetID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:          "public",
	})
	require.NoError(t, err)
	stampRemoteSessionIssuer(t, ctx, ti.conn, projectID, server.ID, remoteIssuerID)
	attachMetaMemberRow(t, ctx, ti.conn, projectID, metaID, server.ID, 0)
	return server.ID, remote.ID
}

func saveServerKey(t *testing.T, ti *testInstance, projectID, remoteServerID uuid.UUID, key string) {
	t.Helper()
	_, err := remotemcp.NewHeaders(testenv.NewLogger(t), ti.conn, ti.enc).CreateServerHeader(t.Context(), remotemcprepo.CreateServerHeaderParams{
		RemoteMcpServerID: remoteServerID, ProjectID: projectID, Name: "X-Api-Key", IsRequired: true, IsSecret: true, Value: conv.ToPGText(key),
	})
	require.NoError(t, err)
}

// A person refused on a member's configured key connects first when it has a
// sign-in, and otherwise asks an admin; a key that never arrived changes nothing.
func TestServePublic_MetaEndpoint_ExecuteTool_SavedKeyRefusedAsksAnAdmin(t *testing.T) {
	t.Parallel()

	const (
		refused      = `server "member" refused the credential configured for it in Speakeasy, such as an API key. Connecting does not change it. Ask an admin to update the server's credential, then retry`
		forbidden    = `server "member" answered 403 Forbidden to a request carrying the credential configured for it in Speakeasy, such as an API key. The credential's account may not be allowed to do this. Connecting does not change that. Ask an admin to check the server's credential and what its account may do, then retry`
		connectFirst = `server "member" refused the request. Ask the user to open {page} to connect it, then retry. If it still fails, ask an admin to check the credential configured for the server in Speakeasy`
		noKey        = `server "member" requires authentication and has no credential for this caller. Ask your administrator to connect it`
		bearer       = `Bearer resource_metadata="https://example.com/.well-known/oauth-protected-resource"`
	)
	for _, tc := range []struct {
		name      string
		signIn    bool
		status    int
		challenge string
		when      upstreamRefusal
		want      string
		recorded  bool
	}{
		{name: "no sign-in, 401", status: http.StatusUnauthorized, want: refused, recorded: true},
		{name: "no sign-in, 401 on tools/call after a live session", status: http.StatusUnauthorized, when: refuseToolsCall, want: refused, recorded: true},
		{name: "no sign-in, 403", status: http.StatusForbidden, want: forbidden},
		{name: "no sign-in, 403 invalid token", status: http.StatusForbidden, challenge: `Bearer error="invalid_token"`, want: refused, recorded: true},
		{name: "no sign-in, key stripped by a redirect", status: http.StatusUnauthorized, when: refuseAfterRedirect, want: noKey},
		{name: "sign-in not connected, 401", signIn: true, status: http.StatusUnauthorized, want: connectFirst, recorded: true},
		{name: "sign-in not connected, 401 bearer challenge", signIn: true, status: http.StatusUnauthorized, challenge: bearer, want: connectFirst, recorded: true},
		{name: "sign-in not connected, 403", signIn: true, status: http.StatusForbidden, want: connectFirst},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			projectID := *authCtx.ProjectID
			orgID := authCtx.ActiveOrganizationID

			sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
			metaSlug := "meta-key-" + uuid.NewString()[:8]
			meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)
			upstream := newKeyRefusingUpstream(t, tc.status, tc.challenge, tc.when)
			remoteIssuer := uuid.NullUUID{}
			if tc.signIn {
				clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-key", "", []uuid.UUID{sharedIssuerID})
				remoteIssuer = conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID))
			}
			serverID, remoteID := createKeyMember(t, ctx, ti, projectID, meta.ID, upstream.URL, remoteIssuer)
			saveServerKey(t, ti, projectID, remoteID, "saved-key")
			subject := createTestUser(t, ctx, ti, "meta-key-user-"+uuid.NewString())
			token := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)

			text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, token, "member--ping"))
			require.True(t, isError)
			message, reference, _ := strings.Cut(text, "\nReference: ")
			page := ti.serverURL.String() + "/mcp/" + metaSlug + "/connect/first-party?server=member"
			require.Equal(t, strings.ReplaceAll(tc.want, "{page}", page), message)
			require.Equal(t, tc.recorded, reference != "", "a recorded row is referenced")
			if tc.when == refuseAfterRedirect {
				upstream.requireRefused(t, "")
			} else {
				upstream.requireRefused(t, "saved-key")
			}

			if !tc.recorded {
				rec.RequireNothing()
				return
			}
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStaticKeyRejected)
			require.Equal(t, serverID.String(), row.MemberID)
			require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
			require.Len(t, rec.Rows(), 1)
		})
	}
}

// An agent refused on its member's configured key is told it cannot change it.
func TestAgentGateway_SavedKeyRefusedAsksAnAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	ti.service.manual = true
	fx := seedAgentGateway(t, ctx, ti)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *ac.ProjectID

	upstream := newKeyRefusingUpstream(t, http.StatusUnauthorized, "", refuseAll)
	remote := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcprepo.CreateServerParams{ProjectID: projectID, TransportType: "streamable-http", Url: upstream.URL})
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	serverID := mcpServerBySlug(t, ctx, ti, projectID, fx.granted)
	_, err := mcpserversrepo.New(ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{ID: serverID, ProjectID: projectID, Name: conv.ToPGText(fx.granted), Slug: conv.ToPGText(fx.granted), RemoteMcpServerID: conv.ToNullUUID(remote.ID), UserSessionIssuerID: conv.ToNullUUID(issuerID), Visibility: mcpservers.VisibilityPrivate})
	require.NoError(t, err)
	saveServerKey(t, ti, projectID, remote.ID, "saved-key")
	fx.token = issueMemberAuthRegressionKey(t, ctx, ti, fx, serverID)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute_tool", "arguments": map[string]any{"name": fx.granted + "--ping", "arguments": map[string]any{}}})
	response, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, body)
	require.NoError(t, err)
	text, isError := metaToolResultText(t, decodeRPCResponse(t, response))
	require.True(t, isError)
	message, _, _ := strings.Cut(text, "\nReference: ")
	require.Equal(t, `server "`+fx.granted+`" refused the credential configured for it in Speakeasy, such as an API key. This agent cannot change it. Ask an admin to update the server's credential, then retry`, message)
	upstream.requireRefused(t, "saved-key")
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStaticKeyRejected)
	require.Equal(t, serverID.String(), row.MemberID)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}
