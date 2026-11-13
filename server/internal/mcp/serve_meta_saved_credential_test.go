package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// keyRefusingUpstream answers every request with status, and challenge as its
// WWW-Authenticate when set, recording each X-Api-Key it received.
type keyRefusingUpstream struct {
	*httptest.Server

	mu   sync.Mutex
	keys []string
}

func newKeyRefusingUpstream(t *testing.T, status int, challenge string) *keyRefusingUpstream {
	t.Helper()
	u := &keyRefusingUpstream{Server: nil, mu: sync.Mutex{}, keys: nil}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.keys = append(u.keys, r.Header.Get("X-Api-Key"))
		u.mu.Unlock()
		if challenge != "" {
			w.Header().Set("WWW-Authenticate", challenge)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *keyRefusingUpstream) requireSaw(t *testing.T, key string) {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	require.Contains(t, u.keys, key, "the saved key must reach the upstream")
}

func saveServerKey(t *testing.T, ti *testInstance, projectID, remoteServerID uuid.UUID, key string) {
	t.Helper()
	_, err := remotemcp.NewHeaders(testenv.NewLogger(t), ti.conn, ti.enc).CreateServerHeader(t.Context(), remotemcprepo.CreateServerHeaderParams{
		RemoteMcpServerID: remoteServerID, ProjectID: projectID, Name: "X-Api-Key", IsRequired: true, IsSecret: true, Value: conv.ToPGText(key),
	})
	require.NoError(t, err)
}

// A person whose gateway member sends the key an admin saved on it, and is
// refused, is told an admin updates the key. Connecting would not fix it, so
// the message does not ask for that, and a 403 does not claim the key is wrong.
func TestServePublic_MetaEndpoint_ExecuteTool_SavedKeyRefusedAsksAnAdmin(t *testing.T) {
	t.Parallel()

	const (
		rejected = `server "member" rejected the credential configured for it in Speakeasy, such as an API key. Signing in does not change it. Ask an admin of the server's project to update the server's credential, then retry`
		refused  = `server "member" refused the credential configured for it in Speakeasy, such as an API key: the credential may be wrong, or its account may not be allowed to do this. Signing in does not change it. Ask an admin of the server's project to check the server's credential and what its account may do, then retry`
	)
	for _, tc := range []struct {
		name      string
		signIn    bool
		status    int
		challenge string
		want      string
		recorded  bool
	}{
		{name: "no sign-in, 401", status: http.StatusUnauthorized, want: rejected, recorded: true},
		{name: "sign-in not connected, 401", signIn: true, status: http.StatusUnauthorized, want: rejected, recorded: true},
		{name: "no sign-in, 403", status: http.StatusForbidden, want: refused},
		{name: "sign-in not connected, 403", signIn: true, status: http.StatusForbidden, want: refused},
		{name: "403 invalid token", status: http.StatusForbidden, challenge: `Bearer error="invalid_token"`, want: refused, recorded: true},
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
			upstream := newKeyRefusingUpstream(t, tc.status, tc.challenge)
			remoteIssuer := uuid.NullUUID{}
			if tc.signIn {
				clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-key", "", []uuid.UUID{sharedIssuerID})
				remoteIssuer = conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID))
			}
			member := createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", upstream.URL, remoteIssuer, 0)
			saveServerKey(t, ti, projectID, member.remoteServerID, "saved-key")
			subject := createTestUser(t, ctx, ti, "meta-key-user-"+uuid.NewString())
			bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)

			text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
			require.True(t, isError)
			message, reference, _ := strings.Cut(text, "\nReference: ")
			require.Equal(t, tc.want, message)
			require.Equal(t, tc.recorded, reference != "", "a recorded row is referenced")
			upstream.requireSaw(t, "saved-key")

			if !tc.recorded {
				rec.RequireNothing()
				return
			}
			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStaticKeyRejected)
			require.Equal(t, member.mcpServerID.String(), row.MemberID)
			require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
			require.Len(t, rec.Rows(), 1)
		})
	}
}

// An agent refused on the key saved on its member is told it cannot change
// the key, rather than to sign in again.
func TestAgentGateway_SavedKeyRefusedAsksAnAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	ti.service.manual = true
	fx := seedAgentGateway(t, ctx, ti)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *ac.ProjectID

	upstream := newKeyRefusingUpstream(t, http.StatusUnauthorized, "")
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
	require.Equal(t, `server "`+fx.granted+`" rejected the credential configured for it in Speakeasy, such as an API key. This agent cannot change it. Ask an admin of the server's project to update the server's credential, then retry`, message)
	upstream.requireSaw(t, "saved-key")
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeStaticKeyRejected)
	require.Equal(t, serverID.String(), row.MemberID)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}
