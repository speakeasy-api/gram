package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// memberRefreshOutageFixture is a gateway member whose user holds an expired
// connection with a live refresh token, and a provider that can be taken down.
type memberRefreshOutageFixture struct {
	ti       *testInstance
	metaSlug string
	bearer   string
	subject  urn.SessionSubject
	clientID uuid.UUID
	down     *atomic.Bool
	upstream *atomic.Value

	// memberCalls counts MCP calls that reached the member.
	memberCalls *atomic.Int32

	// refreshes counts POSTs to the provider's token endpoint.
	refreshes *atomic.Int32
}

func (f memberRefreshOutageFixture) endCooldown(t *testing.T, ctx context.Context) {
	t.Helper()
	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	active, err := remotesessions_repo.New(f.ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: f.subject, RemoteSessionClientID: f.clientID})
	require.NoError(t, err)
	require.NoError(t, cache.NewRedisCacheAdapter(client).Delete(ctx, remotesessions.RefreshCooldownKey(active)))
}

func newMemberRefreshOutageFixture(t *testing.T, ctx context.Context, ti *testInstance, resource func(upstreamURL string) string) memberRefreshOutageFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	down := &atomic.Bool{}
	refreshes := &atomic.Int32{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshes.Add(1)
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"refreshed-token","token_type":"Bearer","expires_in":3600,"refresh_token":"rotated-refresh"}`)
	}))
	t.Cleanup(provider.Close)

	memberCalls := &atomic.Int32{}
	lastAuthorization := &atomic.Value{}
	lastAuthorization.Store("")
	// The protected resource prober GETs the member on its own schedule; only MCP calls count here.
	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			memberCalls.Add(1)
			lastAuthorization.Store(r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(member.Close)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-outage-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "outage-"+uuid.NewString()[:8], provider.URL, []uuid.UUID{issuerID})
	remoteIssuerID := clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", member.URL, conv.ToNullUUID(remoteIssuerID), 0)
	subject := createTestUser(t, ctx, ti, "meta-outage-user-"+uuid.NewString())

	access, err := ti.enc.Encrypt([]byte("expired-token"))
	require.NoError(t, err)
	refresh, err := ti.enc.Encrypt([]byte("live-refresh"))
	require.NoError(t, err)
	stored := resource(member.URL)
	_, err = remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   issuerID,
		RemoteSessionClientID: clientID,
		AccessTokenEncrypted:  access,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
		RefreshTokenEncrypted: conv.ToPGText(refresh),
		RefreshExpiresAt:      pgtype.Timestamptz{Valid: false},
		Scopes:                []string{},
		Resource:              pgtype.Text{String: stored, Valid: stored != ""},
	})
	require.NoError(t, err)

	return memberRefreshOutageFixture{
		ti:       ti,
		metaSlug: metaSlug,
		bearer:   mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject),
		subject:  subject,
		clientID: clientID,
		down:     down,
		upstream: lastAuthorization,

		memberCalls: memberCalls,
		refreshes:   refreshes,
	}
}

func (f memberRefreshOutageFixture) connectionStatus(t *testing.T, ctx context.Context) metamcp.ConnectionStatusResult {
	t.Helper()

	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": metamcp.ToolConnectionStatus, "arguments": map[string]any{}})
	resp, err := servePublicHTTP(t, ctx, f.ti, f.metaSlug, body, f.bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, "body=%s", resp.Body.String())

	var res struct {
		StructuredContent metamcp.ConnectionStatusResult `json:"structuredContent"`
	}
	require.NoError(t, json.Unmarshal(decodeRPCResponse(t, resp)["result"], &res))
	return res.StructuredContent
}

func TestServePublic_MetaEndpoint_RefreshOutageIsRetryableThenRecovers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newMemberRefreshOutageFixture(t, ctx, ti, func(upstream string) string { return upstream })
	f.down.Store(true)

	text, isError := metaToolResultText(t, executeMetaTool(t, ti, f.metaSlug, f.bearer, "member--ping"))
	require.True(t, isError)
	require.Contains(t, text, "Retry in about 30 seconds")
	require.Contains(t, text, "If it keeps happening, reconnect at")
	require.Contains(t, text, "/connect/first-party?server=member", "reconnecting stays offered, since a provider can answer 5xx for one bad token")
	require.NotContains(t, text, "not connected")
	require.Zero(t, f.memberCalls.Load(), "an outage never turns into an anonymous call")
	require.EqualValues(t, 1, f.refreshes.Load())

	status := f.connectionStatus(t, ctx)
	require.Len(t, status.Servers, 1)
	require.Equal(t, metamcp.SignInUnavailable, status.Servers[0].Status)
	require.NotContains(t, status.Text(), "To sign in, open")
	require.Contains(t, status.Servers[0].Detail, "reconnect at")
	require.EqualValues(t, 1, f.refreshes.Load(), "retries inside the cooldown never reach the provider")

	stored, err := remotesessions_repo.New(ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
		SubjectUrn:            f.subject,
		RemoteSessionClientID: f.clientID,
	})
	require.NoError(t, err)
	require.True(t, stored.RefreshTokenEncrypted.Valid, "the outage must not delete the refresh token")

	f.down.Store(false)
	f.endCooldown(t, ctx)
	text, isError = metaToolResultText(t, executeMetaTool(t, ti, f.metaSlug, f.bearer, "member--ping"))
	require.True(t, isError)
	require.Contains(t, text, "rejected the user's credential", "after the outage the member receives the refreshed token")
	require.Equal(t, "Bearer refreshed-token", f.upstream.Load())
	require.Equal(t, metamcp.SignInConnected, f.connectionStatus(t, ctx).Servers[0].Status)
}

// A grant from before resources were recorded still belongs to its member.
func TestServePublic_MetaEndpoint_RefreshOutageWithoutRecordedResourceIsRetryable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newMemberRefreshOutageFixture(t, ctx, ti, func(string) string { return "" })
	f.down.Store(true)

	text, isError := metaToolResultText(t, executeMetaTool(t, ti, f.metaSlug, f.bearer, "member--ping"))
	require.True(t, isError)
	require.Contains(t, text, "Retry in about")
	require.NotContains(t, text, "not connected")
}

// A revocation store outage judges nothing: the app keeps its token and retries
// the same request, which works once the store is back.
func TestServePublic_RevocationCheckOutageIsRetryableThenRecovers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newMemberRefreshOutageFixture(t, ctx, ti, func(upstream string) string { return upstream })
	body := makeMetaRPCBody(t, "tools/list", map[string]any{})

	broken := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1})
	require.NoError(t, broken.Close())
	ti.service.SetChatSessionsManager(chatsessions.NewManager(testenv.NewLogger(t), broken, "test-jwt-secret"))

	w, err := servePublicHTTP(t, ctx, ti, f.metaSlug, body, f.bearer, nil)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.NotEmpty(t, w.Header().Get("Retry-After"))
	require.Empty(t, w.Header().Get("WWW-Authenticate"), "an outage never restarts sign-in")
	require.Empty(t, w.Header().Get(constants.HeaderProxiedResponse), "our own dependency stays counted")

	ti.service.SetChatSessionsManager(ti.chatSessionsManager)
	w, err = servePublicHTTP(t, ctx, ti, f.metaSlug, body, f.bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// A database that can't answer reads as an outage on every surface an app
// restarts sign-in from, and the same bearer works once it's back.
func TestDatabaseOutageAnswersRetryably(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	f := newMemberRefreshOutageFixture(t, ctx, ti, func(upstream string) string { return upstream })
	dead, err := pgxpool.New(ctx, "postgres://speakeasy@127.0.0.1:1/down?connect_timeout=1")
	require.NoError(t, err)
	t.Cleanup(dead.Close)
	ti.service.SetDB(dead)

	resource := func() *httptest.ResponseRecorder {
		req := newTunneledPublicRequest(f.metaSlug, http.MethodPost, makeMetaRPCBody(t, "tools/list", map[string]any{}), "")
		req.Header.Set("Authorization", "Bearer "+f.bearer)
		w := httptest.NewRecorder()
		oops.MCPErrHandle(testenv.NewLogger(t), ti.service.ServePublic).ServeHTTP(w, req)
		return w
	}
	down := resource()
	require.Equal(t, http.StatusServiceUnavailable, down.Code, down.Body.String())
	require.NotEmpty(t, down.Header().Get("Retry-After"))
	require.Empty(t, down.Header().Get("WWW-Authenticate"))
	require.Empty(t, down.Header().Get(constants.HeaderProxiedResponse), "our own failure keeps alerting")

	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {"any"}, "refresh_token": {"any"}}
	tokenReq := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/"+f.metaSlug+"/token", strings.NewReader(form.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", f.metaSlug)
	token := httptest.NewRecorder()
	oops.OAuthErrHandle(testenv.NewLogger(t), ti.service.HandleToken).ServeHTTP(token, tokenReq.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx)))
	require.Equal(t, http.StatusServiceUnavailable, token.Code, token.Body.String())
	require.NotEmpty(t, token.Header().Get("Retry-After"))
	require.Contains(t, token.Body.String(), "temporarily_unavailable")

	discovery := httptest.NewRecorder()
	oops.AuthErrHandle(testenv.NewLogger(t), ti.service.HandleGetProtectedResource).ServeHTTP(discovery, wellKnownRequest(t, "/.well-known/oauth-protected-resource/mcp/", f.metaSlug))
	require.Equal(t, http.StatusServiceUnavailable, discovery.Code, discovery.Body.String())
	require.NotEmpty(t, discovery.Header().Get("Retry-After"))

	shared := httptest.NewRecorder()
	oops.OAuthErrHandle(testenv.NewLogger(t), ti.service.HandleSharedToken).ServeHTTP(shared, sharedRequest(ctx, http.MethodPost, "/oauth/usi/"+uuid.NewString()+"/token", uuid.NewString(), strings.NewReader(form.Encode())))
	require.Equal(t, http.StatusServiceUnavailable, shared.Code, shared.Body.String())
	require.Contains(t, shared.Body.String(), "temporarily_unavailable")

	ti.service.SetDB(ti.conn)
	up := resource()
	require.Equal(t, http.StatusOK, up.Code, up.Body.String())

	missing := httptest.NewRecorder()
	oops.OAuthErrHandle(testenv.NewLogger(t), ti.service.HandleSharedToken).ServeHTTP(missing, sharedRequest(ctx, http.MethodPost, "/oauth/usi/"+uuid.NewString()+"/token", uuid.NewString(), strings.NewReader(form.Encode())))
	require.NotEqual(t, http.StatusServiceUnavailable, missing.Code, "a missing row stays as it was: %s", missing.Body.String())
}

// A failed admission read reached no verdict, so an agent keeps its refresh
// token: an outage is retryable, a broken deploy is our 500, neither says reauthorize.
func TestHandleToken_AgentRefreshDuringAdmissionFailure(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		code  string
		want  int
		retry bool
	}{
		{name: "outage", code: "57P01", want: http.StatusServiceUnavailable, retry: true},
		{name: "broken deploy", code: "42P01", want: http.StatusInternalServerError, retry: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			fx, _, refreshToken, _ := seedAgentRefreshSession(t, ctx, ti)
			working := ti.authzEngine
			fail := fmt.Errorf("load admission: %w", &pgconn.PgError{Code: tc.code})
			ti.service.SetAuthzEngine(authz.NewEngine(testenv.NewLogger(t), ti.conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{
				AdmitPrincipalCredential: func(context.Context, *pgxpool.Pool) (authz.PrincipalCredentialAdmission, error) {
					return authz.PrincipalCredentialAdmission{}, fail
				},
				AdmitPrincipalCredentialWithDBTX: func(context.Context, accessrepo.DBTX) (authz.PrincipalCredentialAdmission, error) {
					return authz.PrincipalCredentialAdmission{}, fail
				},
			}))

			form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {fx.client.ClientID}}
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/"+fx.toolset.McpSlug.String+"/token", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			routeCtx := chi.NewRouteContext()
			routeCtx.URLParams.Add("mcpSlug", fx.toolset.McpSlug.String)
			w := httptest.NewRecorder()
			oops.OAuthErrHandle(testenv.NewLogger(t), ti.service.HandleToken).ServeHTTP(w, req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx)))
			require.Equal(t, tc.want, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "invalid_grant")
			require.Equal(t, tc.retry, w.Header().Get("Retry-After") != "")

			ti.service.SetAuthzEngine(working)
			retried := performRefreshRequest(ctx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, refreshToken)
			require.NoError(t, retried.err)
			require.Equal(t, http.StatusOK, retried.code, retried.body)
		})
	}
}
