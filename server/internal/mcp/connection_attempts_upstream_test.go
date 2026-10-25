package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// upstreamAttemptRow is one raw upstream_callback or upstream_refresh row.
type upstreamAttemptRow struct {
	Stage        string `ch:"stage"`
	Outcome      string `ch:"outcome"`
	FailureClass string `ch:"failure_class"`
	Reason       string `ch:"reason"`
	Subject      string `ch:"subject"`
	ClientKey    string `ch:"client_key"`
	ProviderID   string `ch:"provider_id"`
	Attributes   string `ch:"attributes"`
}

// upstreamAttempts reads a gateway's committed provider rows, oldest first.
func upstreamAttempts(t *testing.T, projectID, gatewayID uuid.UUID) []upstreamAttemptRow {
	t.Helper()
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	var rows []upstreamAttemptRow
	require.NoError(t, chConn.Select(t.Context(), &rows,
		`SELECT toString(attributes.gram.connection_attempt.stage) AS stage,
			        toString(attributes.gram.connection_attempt.outcome) AS outcome,
			        toString(attributes.gram.connection_attempt.failure_class) AS failure_class,
			        toString(attributes.gram.connection_attempt.reason) AS reason,
			        toString(attributes.gram.connection_attempt.subject) AS subject,
			        toString(attributes.gram.connection_attempt.client_key) AS client_key,
			        toString(attributes.gram.connection_attempt.provider_id) AS provider_id,
			        toJSONString(attributes) AS attributes
			 FROM telemetry_logs
			 WHERE gram_project_id = ? AND event_urn = ? AND meta_mcp_server_id = ?
			   AND startsWith(toString(attributes.gram.connection_attempt.stage), 'upstream_')
			 ORDER BY time_unix_nano`,
		projectID.String(), mcp.MCPConnectionAttemptEventURN, gatewayID.String()))
	return rows
}

// providerTokenEndpoint is an owned authorization server whose /token answer
// each test chooses.
type providerTokenEndpoint struct {
	server *httptest.Server
	answer func(w http.ResponseWriter, r *http.Request)
}

func newProviderTokenEndpoint(t *testing.T) *providerTokenEndpoint {
	t.Helper()
	p := &providerTokenEndpoint{server: nil, answer: nil}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" || p.answer == nil {
			http.NotFound(w, r)
			return
		}
		p.answer(w, r)
	}))
	t.Cleanup(p.server.Close)
	return p
}

func tokenAnswer(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// gatewayCallbackFixture is a configured gateway whose sign-in names a
// registered client, one provider connection to an owned token endpoint, and
// the browser cookie the person's sign-in is bound to.
type gatewayCallbackFixture struct {
	ti         *testInstance
	endpoint   *mcp.ResolvedMcpEndpoint
	projectID  uuid.UUID
	gatewayID  uuid.UUID
	inbound    usersessions_repo.UserSessionClient
	provider   uuid.UUID
	issuerID   uuid.UUID
	remote     uuid.UUID
	token      *providerTokenEndpoint
	cookieName string
	cookie     string
}

func seedGatewayCallback(t *testing.T, slug string) (context.Context, gatewayCallbackFixture) {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *auth.ProjectID, auth.ActiveOrganizationID
	issuer := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, slug, issuer)
	inbound, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		ClientID: "callback-client-" + uuid.NewString(), ClientSecretHash: pgtype.Text{}, ClientName: "Example Desktop",
		RedirectUris: []string{"http://127.0.0.1/callback"}, ClientSecretExpiresAt: pgtype.Timestamptz{},
		TokenEndpointAuthMethod: "none", ClientJwks: nil, ClientJwksUri: pgtype.Text{}, UserSessionIssuerID: issuer,
	})
	require.NoError(t, err)
	token := newProviderTokenEndpoint(t)
	remote := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, slug, token.server.URL, []uuid.UUID{issuer})
	row, err := remotesessions_repo.New(ti.conn).GetRemoteSessionClientByID(ctx, remotesessions_repo.GetRemoteSessionClientByIDParams{ID: remote, ProjectID: projectID, OrganizationID: orgID})
	require.NoError(t, err)
	// The gateway offers a provider only through a member configured for it.
	member := seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gateway.ID, "Tracker", "tracker-"+uuid.NewString()[:8], 0, newRecordingUpstream(t, "ping").url)
	_, err = upstreamclientsrepo.New(ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: orgID, ProjectID: projectID, McpServerID: member, RemoteSessionClientID: remote})
	require.NoError(t, err)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, slug, "mcp")
	require.NoError(t, err)
	require.True(t, endpoint.MetaMcpServerID.Valid)
	cookieID := uuid.NewString()
	return ctx, gatewayCallbackFixture{
		ti: ti, endpoint: endpoint, projectID: projectID, gatewayID: gateway.ID, inbound: inbound,
		provider: row.RemoteSessionClient.RemoteSessionIssuerID, issuerID: issuer, remote: remote, token: token,
		cookieName: "__Host-gram-federation-" + cookieID, cookie: "browser-" + uuid.NewString(),
	}
}

// startSignIn stores the person's bound sign-in challenge and sends its
// connect action, returning the person and the provider authorize redirect.
func (fx gatewayCallbackFixture) startSignIn(t *testing.T, ctx context.Context, clientID string) (urn.SessionSubject, *url.URL) {
	t.Helper()
	return fx.startSignInAs(t, ctx, clientID, uuid.NullUUID{UUID: fx.inbound.ID, Valid: true})
}

// startSignInAs is startSignIn for a challenge that recorded registered as
// its validated client, or none.
func (fx gatewayCallbackFixture) startSignInAs(t *testing.T, ctx context.Context, clientID string, registered uuid.NullUUID) (urn.SessionSubject, *url.URL) {
	t.Helper()
	sum := sha256.Sum256([]byte(fx.cookie))
	hash := base64.RawURLEncoding.EncodeToString(sum[:])
	subject := createTestUser(t, ctx, fx.ti, uuid.NewString())
	stateID := uuid.NewString()
	require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: fx.issuerID,
		Endpoint:            mcp.EndpointRef{McpSlug: fx.endpoint.Slug, RouteBase: fx.endpoint.RouteBase, CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}},
		ClientID:            clientID,
		RegisteredClientID:  registered,
		RedirectURI:         "http://127.0.0.1/callback",
		CSRFToken:           "csrf-token",
		Subject:             &subject,
		CreatedAt:           time.Now(),
		Browser:             &mcp.ChallengeBrowserBinding{CookieID: strings.TrimPrefix(fx.cookieName, "__Host-gram-federation-"), OriginHash: hash, CallbackHash: hash, CallbackOrigin: ""},
	}))
	form := url.Values{"state": {stateID}, "csrf_token": {"csrf-token"}, "action": {"connect"}, "client_id": {fx.remote.String()}}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: fx.cookieName, Value: fx.cookie})
	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsentAction(w, req, fx.endpoint))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	return subject, loc
}

// callback returns to the gateway's own callback route, browser cookie
// included, with the given query beside the login's state.
func (fx gatewayCallbackFixture) callback(ctx context.Context, state string, query url.Values) error {
	_, err := fx.callbackRedirect(ctx, state, query)
	return err
}

// callbackRedirect is callback returning where the gateway sent the browser.
func (fx gatewayCallbackFixture) callbackRedirect(ctx context.Context, state string, query url.Values) (string, error) {
	query.Set("state", state)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/mcp/remote_login_callback?"+query.Encode(), nil)
	req.AddCookie(&http.Cookie{Name: fx.cookieName, Value: fx.cookie})
	w := httptest.NewRecorder()
	if err := fx.ti.service.HandleRemoteLoginCallback(w, req); err != nil {
		return "", fmt.Errorf("remote login callback: %w", err)
	}
	return w.Header().Get("Location"), nil
}

// A stored callback names the person, the registered client they signed in
// to the gateway with, and the provider, only once the credential is saved.
// A callback whose state is missing names no gateway and records nothing.
func TestGatewayProviderCallbackStored(t *testing.T) {
	t.Parallel()
	ctx, fx := seedGatewayCallback(t, "callback-stored-"+uuid.NewString()[:8])
	writes := watchConnectionAttempts(t, fx.ti)
	fx.token.answer = tokenAnswer(http.StatusOK, `{"access_token":"provider-access-secret","token_type":"Bearer","expires_in":3600,"refresh_token":"provider-refresh-secret"}`)

	require.Error(t, fx.callback(ctx, uuid.NewString(), url.Values{"code": {"code-for-missing-state"}}))

	subject, loc := fx.startSignIn(t, ctx, fx.inbound.ClientID)
	state := loc.Query().Get("state")
	require.NoError(t, fx.callback(ctx, state, url.Values{"code": {"provider-code-secret"}}))
	require.Equal(t, 1, writes.written(t))

	_, err := remotesessions_repo.New(fx.ti.conn).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: fx.remote})
	require.NoError(t, err, "stored means the credential was committed")
	rows := upstreamAttempts(t, fx.projectID, fx.gatewayID)
	require.Len(t, rows, 1, "the callback with missing state is not recorded")
	require.Equal(t, upstreamAttemptRow{Stage: "upstream_callback", Outcome: "stored", FailureClass: "", Reason: "", Subject: subject.String(), ClientKey: fx.inbound.ID.String(), ProviderID: fx.provider.String(), Attributes: rows[0].Attributes}, rows[0])
	require.NotEqual(t, fx.remote.String(), rows[0].ClientKey, "the provider client is never the gateway client")
	for _, secret := range []string{"provider-code-secret", "provider-access-secret", "provider-refresh-secret", state, fx.cookie, fx.token.server.URL} {
		require.NotContains(t, rows[0].Attributes, secret)
	}
}

// A provider denial ends before the callback loads the provider client, yet
// it is still the stamped person's, through their registered client, at that
// provider; the provider's description is never recorded.
func TestGatewayProviderCallbackDenied(t *testing.T) {
	t.Parallel()
	ctx, fx := seedGatewayCallback(t, "callback-denied-"+uuid.NewString()[:8])
	writes := watchConnectionAttempts(t, fx.ti)

	subject, loc := fx.startSignIn(t, ctx, fx.inbound.ClientID)
	require.Error(t, fx.callback(ctx, loc.Query().Get("state"), url.Values{"error": {"access_denied"}, "error_description": {"vendor-description-secret"}}))
	_, loc = fx.startSignIn(t, ctx, fx.inbound.ClientID)
	require.Error(t, fx.callback(ctx, loc.Query().Get("state"), url.Values{"error": {"temporarily_unavailable"}}))
	require.Equal(t, 1, writes.written(t))
	require.Equal(t, 1, writes.written(t))

	rows := upstreamAttempts(t, fx.projectID, fx.gatewayID)
	require.Len(t, rows, 2)
	require.Equal(t, []string{"failed", "configuration", "access_denied", subject.String(), fx.inbound.ID.String(), fx.provider.String()},
		[]string{rows[0].Outcome, rows[0].FailureClass, rows[0].Reason, rows[0].Subject, rows[0].ClientKey, rows[0].ProviderID})
	require.Equal(t, []string{"failed", "endpoint", "temporarily_unavailable", fx.provider.String()},
		[]string{rows[1].Outcome, rows[1].FailureClass, rows[1].Reason, rows[1].ProviderID})
	require.NotContains(t, rows[0].Attributes, "vendor-description-secret")
}

// Exchange failures are classified by who failed: a provider outage counts
// against its endpoint and a refused code against configuration; the
// provider's error body is never recorded.
func TestGatewayProviderCallbackExchangeFailures(t *testing.T) {
	t.Parallel()
	ctx, fx := seedGatewayCallback(t, "callback-exchange-"+uuid.NewString()[:8])
	writes := watchConnectionAttempts(t, fx.ti)

	for _, answer := range []func(http.ResponseWriter, *http.Request){
		tokenAnswer(http.StatusServiceUnavailable, `{"error_description":"vendor-outage-secret"}`),
		tokenAnswer(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"vendor-grant-secret"}`),
		tokenAnswer(http.StatusOK, `{"error":"server_error"}`),
	} {
		fx.token.answer = answer
		_, loc := fx.startSignIn(t, ctx, fx.inbound.ClientID)
		require.Error(t, fx.callback(ctx, loc.Query().Get("state"), url.Values{"code": {"code"}}))
		require.Equal(t, 1, writes.written(t))
	}

	rows := upstreamAttempts(t, fx.projectID, fx.gatewayID)
	require.Len(t, rows, 3)
	got := make([][3]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, [3]string{row.Outcome, row.FailureClass, row.Reason})
		require.Equal(t, fx.provider.String(), row.ProviderID)
		require.NotContains(t, row.Attributes, "vendor-")
	}
	require.Equal(t, [][3]string{
		{"failed", "endpoint", "provider_unavailable"},
		{"failed", "configuration", "invalid_grant"},
		{"failed", "endpoint", "provider_server_error"},
	}, got)
}

// A login minted before states carried the person's registered client, or
// one whose client no longer resolves, names no client rather than guessing.
func TestGatewayProviderCallbackWithoutInboundClient(t *testing.T) {
	t.Parallel()
	ctx, fx := seedGatewayCallback(t, "callback-inflight-"+uuid.NewString()[:8])
	writes := watchConnectionAttempts(t, fx.ti)
	fx.token.answer = tokenAnswer(http.StatusOK, `{"access_token":"a","token_type":"Bearer","expires_in":3600}`)

	subject, loc := fx.startSignIn(t, ctx, fx.inbound.ClientID)
	state := loc.Query().Get("state")
	loginCache := cache.NewTypedObjectCache[remotesessions.RemoteLoginState](fx.ti.logger, fx.ti.cacheAdapter, cache.SuffixNone)
	minted, err := loginCache.Get(ctx, "remoteLogin:"+state)
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: fx.inbound.ID, Valid: true}, minted.InboundUserSessionClientID)
	minted.InboundUserSessionClientID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	require.NoError(t, loginCache.Store(ctx, minted))

	require.NoError(t, fx.callback(ctx, state, url.Values{"code": {"code"}}))
	require.Equal(t, 1, writes.written(t))

	// A challenge that recorded no validated client, such as one minted
	// before it was recorded, leaves the login without one too.
	other, loc := fx.startSignInAs(t, ctx, fx.inbound.ClientID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	require.NoError(t, fx.callback(ctx, loc.Query().Get("state"), url.Values{"code": {"code"}}))
	require.Equal(t, 1, writes.written(t))

	rows := upstreamAttempts(t, fx.projectID, fx.gatewayID)
	require.Len(t, rows, 2)
	require.Equal(t, []string{"stored", subject.String(), "unverified_client"}, []string{rows[0].Outcome, rows[0].Subject, rows[0].ClientKey})
	require.Equal(t, []string{"stored", other.String(), "unverified_client"}, []string{rows[1].Outcome, rows[1].Subject, rows[1].ClientKey})
}

// A callback canceled while exchanging the code is the caller's, recorded
// after the request ended.
func TestGatewayProviderCallbackCanceled(t *testing.T) {
	t.Parallel()
	ctx, fx := seedGatewayCallback(t, "callback-cancel-"+uuid.NewString()[:8])
	writes := watchConnectionAttempts(t, fx.ti)
	requestCtx, cancel := context.WithCancel(ctx)
	// The provider is reached and the caller gives up; the handler returns
	// at once so the server never waits on it.
	fx.token.answer = func(http.ResponseWriter, *http.Request) { cancel() }

	_, loc := fx.startSignIn(t, ctx, fx.inbound.ClientID)
	require.Error(t, fx.callback(requestCtx, loc.Query().Get("state"), url.Values{"code": {"code"}}))
	require.Equal(t, 1, writes.written(t))
	rows := upstreamAttempts(t, fx.projectID, fx.gatewayID)
	require.Len(t, rows, 1)
	require.Equal(t, []string{"failed", "canceled", "canceled"}, []string{rows[0].Outcome, rows[0].FailureClass, rows[0].Reason})
}

// gatewayRefreshFixture is a configured gateway with one proxied member whose
// provider credential refreshes at an owned token endpoint, and a second
// gateway sharing the same sign-in.
type gatewayRefreshFixture struct {
	ti        *testInstance
	projectID uuid.UUID
	slug      string
	gatewayID uuid.UUID
	siblingID uuid.UUID
	issuerID  uuid.UUID
	remote    uuid.UUID
	provider  uuid.UUID
	upstream  *recordingUpstream
	token     *providerTokenEndpoint
	posts     *atomic.Int64
}

func seedGatewayRefresh(t *testing.T, prefix string) (context.Context, gatewayRefreshFixture) {
	t.Helper()
	ctx, ti := newTestMCPService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID, orgID := *auth.ProjectID, auth.ActiveOrganizationID
	issuer := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := prefix + "-" + uuid.NewString()[:8]
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, slug, issuer)
	sibling := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, slug+"-sibling", issuer)
	upstream := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, projectID, gateway.ID, "Member", "member", 0, upstream.url)
	token := newProviderTokenEndpoint(t)
	posts := &atomic.Int64{}
	remote := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, slug, token.server.URL, []uuid.UUID{issuer})
	row, err := remotesessions_repo.New(ti.conn).GetRemoteSessionClientByID(ctx, remotesessions_repo.GetRemoteSessionClientByIDParams{ID: remote, ProjectID: projectID, OrganizationID: orgID})
	require.NoError(t, err)
	return ctx, gatewayRefreshFixture{
		ti: ti, projectID: projectID, slug: slug, gatewayID: gateway.ID, siblingID: sibling.ID, issuerID: issuer,
		remote: remote, provider: row.RemoteSessionClient.RemoteSessionIssuerID, upstream: upstream, token: token, posts: posts,
	}
}

// answerRefresh counts each refresh POST and answers it with answer.
func (fx gatewayRefreshFixture) answerRefresh(answer func(http.ResponseWriter, *http.Request)) {
	fx.token.answer = func(w http.ResponseWriter, r *http.Request) {
		if r.ParseForm() != nil || r.PostForm.Get("grant_type") != "refresh_token" {
			http.NotFound(w, r)
			return
		}
		fx.posts.Add(1)
		answer(w, r)
	}
}

// person signs a person in to the gateway with a provider credential that
// expires after expiresIn and can be refreshed.
func (fx gatewayRefreshFixture) person(t *testing.T, ctx context.Context, expiresIn time.Duration) (urn.SessionSubject, string) {
	t.Helper()
	subject := createTestUser(t, ctx, fx.ti, "refresh-user-"+uuid.NewString())
	access, err := fx.ti.enc.Encrypt([]byte("stored-access-secret"))
	require.NoError(t, err)
	refresh, err := fx.ti.enc.Encrypt([]byte("stored-refresh-secret"))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(fx.ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn: subject, UserSessionIssuerID: fx.issuerID, RemoteSessionClientID: fx.remote,
		AccessTokenEncrypted: access, AccessExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(expiresIn), Valid: true},
		RefreshTokenEncrypted: pgtype.Text{String: refresh, Valid: true}, AuthorizationExpiresAt: pgtype.Timestamptz{},
		RefreshExpiresAt: pgtype.Timestamptz{}, Scopes: []string{}, Resource: pgtype.Text{String: fx.upstream.url, Valid: true},
		AutoRefresh: false, UpstreamSubject: pgtype.Text{}, UpstreamEmail: pgtype.Text{}, UpstreamDisplayName: pgtype.Text{},
		IdentitySource: pgtype.Text{}, Enrichment: nil,
	})
	require.NoError(t, err)
	return subject, mintMetaIssuerBearer(t, fx.ti, fx.slug, fx.issuerID, subject)
}

// A request that needs an expired provider credential refreshes it once, and
// that refresh is the requesting person's on this gateway alone: the sibling
// gateway sharing the credential records nothing.
func TestGatewayRequestRefreshRecorded(t *testing.T) {
	t.Parallel()
	ctx, fx := seedGatewayRefresh(t, "refresh-ok")
	writes := watchConnectionAttempts(t, fx.ti)
	fx.answerRefresh(tokenAnswer(http.StatusOK, `{"access_token":"refreshed-access-secret","token_type":"Bearer","expires_in":3600,"refresh_token":"rotated-refresh-secret"}`))
	subject, bearer := fx.person(t, ctx, -time.Minute)

	text, isError := metaToolResultText(t, executeMetaTool(t, fx.ti, fx.slug, bearer, "member--ping"))
	require.False(t, isError, text)
	fx.upstream.requireToolCallAuth(t, "Bearer refreshed-access-secret", "the member is called with the refreshed credential")
	require.Equal(t, 1, writes.written(t))

	rows := upstreamAttempts(t, fx.projectID, fx.gatewayID)
	require.Len(t, rows, 1)
	require.Equal(t, []string{"upstream_refresh", "refreshed", "", subject.String(), fx.provider.String()},
		[]string{rows[0].Stage, rows[0].Outcome, rows[0].Reason, rows[0].Subject, rows[0].ProviderID})
	require.NotEqual(t, fx.remote.String(), rows[0].ClientKey, "the provider client is never the gateway client")
	for _, secret := range []string{"refreshed-access-secret", "rotated-refresh-secret", "stored-refresh-secret", bearer, fx.token.server.URL} {
		require.NotContains(t, rows[0].Attributes, secret)
	}
	require.Empty(t, upstreamAttempts(t, fx.projectID, fx.siblingID), "a refresh is never attributed to another gateway")
}

// A refresh the provider refuses inside the expiry skew still serves the
// live credential, and the failure is recorded rather than hidden by it.
func TestGatewayRequestRefreshFailureWhileLive(t *testing.T) {
	t.Parallel()
	ctx, fx := seedGatewayRefresh(t, "refresh-live")
	writes := watchConnectionAttempts(t, fx.ti)
	fx.answerRefresh(tokenAnswer(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"vendor-refresh-secret"}`))
	subject, bearer := fx.person(t, ctx, remotesessions.AccessTokenExpirySkew/2)

	text, isError := metaToolResultText(t, executeMetaTool(t, fx.ti, fx.slug, bearer, "member--ping"))
	require.False(t, isError, text)
	fx.upstream.requireToolCallAuth(t, "Bearer stored-access-secret", "the still-live credential is served")
	require.Equal(t, 1, writes.written(t))

	rows := upstreamAttempts(t, fx.projectID, fx.gatewayID)
	require.Len(t, rows, 1)
	require.Equal(t, []string{"failed", "configuration", "invalid_grant", subject.String(), fx.provider.String()},
		[]string{rows[0].Outcome, rows[0].FailureClass, rows[0].Reason, rows[0].Subject, rows[0].ProviderID})
	require.NotContains(t, rows[0].Attributes, "vendor-refresh-secret")
}

// A provider that refuses the requested server address is asked once more
// without it, which is a retry rather than a stored credential; refusing the
// retry too is the resource's configuration. Both keep the person's
// registered client.
func TestGatewayProviderCallbackRetriesWithoutResource(t *testing.T) {
	t.Parallel()
	ctx, fx := seedGatewayCallback(t, "callback-retry-"+uuid.NewString()[:8])
	writes := watchConnectionAttempts(t, fx.ti)

	subject, loc := fx.startSignIn(t, ctx, fx.inbound.ClientID)
	require.NotEmpty(t, loc.Query().Get("resource"), "the first leg names the member's resource")
	redirect, err := fx.callbackRedirect(ctx, loc.Query().Get("state"), url.Values{"error": {"invalid_target"}})
	require.NoError(t, err)
	require.Equal(t, 1, writes.written(t))
	retry, err := url.Parse(redirect)
	require.NoError(t, err)
	require.Empty(t, retry.Query().Get("resource"), "the retry leg omits the resource")
	require.Error(t, fx.callback(ctx, retry.Query().Get("state"), url.Values{"error": {"invalid_target"}}))
	require.Equal(t, 1, writes.written(t))

	rows := upstreamAttempts(t, fx.projectID, fx.gatewayID)
	require.Len(t, rows, 2)
	got := make([][5]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, [5]string{row.Outcome, row.FailureClass, row.Reason, row.Subject, row.ClientKey})
		require.Equal(t, fx.provider.String(), row.ProviderID)
	}
	require.Equal(t, [][5]string{
		{"retried", "", "", subject.String(), fx.inbound.ID.String()},
		{"failed", "configuration", "resource_rejected", subject.String(), fx.inbound.ID.String()},
	}, got)
}
