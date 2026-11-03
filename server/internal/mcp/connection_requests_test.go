package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The healthy path: an authenticated tools/call forwarded to a working server writes nothing.
func TestHealthyToolCallRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	mockServer := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, []testmcp.Tool{{
		Name:        "ping",
		Description: "Returns pong",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Response:    testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "pong"}}},
	}})
	t.Cleanup(mockServer.Close)
	target, err := url.Parse(mockServer.URL)
	require.NoError(t, err)
	upstream := httptest.NewServer(httputil.NewSingleHostReverseProxy(target))
	t.Cleanup(upstream.Close)

	fixture := createIssuerGatedExternalMCPFixture(t, ctx, ti, authCtx, "healthy-call", upstream.URL)
	subject := urn.NewUserSubject("healthy-user-" + uuid.NewString())
	insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject, "valid-upstream-token", time.Now().Add(time.Hour))
	bearer := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

	resp, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code)

	call, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": fixture.ToolName, "arguments": map[string]any{}}})
	require.NoError(t, err)
	resp, err = servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, call, bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	rec.RequireNothing()
}

// A client's first handshake carries no credentials. It is how sign-in starts.
func TestNoCredentialsRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-no-credentials")

	_, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.Error(t, err)
	rec.RequireNothing()
}

// An expired access token is how a healthy client learns to refresh.
func TestExpiredBearerRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-expired")

	token, _, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  urn.NewUserSubject("expired-user"),
		Audience: urn.NewToolset(fixture.Toolset.ID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + fixture.Toolset.McpSlug.String,
		Lifetime: -time.Minute,
	})
	require.NoError(t, err)

	_, err = servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), token, nil)
	require.Error(t, err)
	rec.RequireNothing()
}

// A bearer of ours names someone. One that is not ours would let a scanner write rows.
func TestRejectedBearer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-rejected")

	_, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), "not-a-token", nil)
	require.Error(t, err)
	rec.RequireNothing()

	// Ours, but minted for another server: it proves nothing about this one.
	subject := urn.NewUserSubject("rejected-user")
	elsewhere, _, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: "urn:some-other-server",
		Issuer:   ti.serverURL.String() + "/mcp/" + fixture.Toolset.McpSlug.String,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	_, err = servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), elsewhere, nil)
	require.Error(t, err)
	rec.RequireNothing()

	token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: urn.NewToolset(fixture.Toolset.ID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + fixture.Toolset.McpSlug.String,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, fixture.UserSessionIssuer.ID, subject, jti)
	require.NoError(t, ti.chatSessionsManager.RevokeToken(ctx, jti))
	_, err = servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), token, nil)
	require.Error(t, err)

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidBearerToken)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
	require.Equal(t, subject.String(), row.SubjectURN)
	require.Equal(t, authCtx.ActiveOrganizationID, row.OrganizationID)
}

func TestRemoteSessionRejections(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		upstream http.HandlerFunc
		want     mcpconnectionattempts.Code
		who      mcpconnectionattempts.WhoFixes
		detail   string
	}{
		{name: "missing", want: mcpconnectionattempts.CodeInvalidRemoteSession, who: mcpconnectionattempts.WhoPerson, detail: "invalid_remote_session"},
		{name: "outage", upstream: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }, want: mcpconnectionattempts.CodeRemoteSessionUnavailable, who: mcpconnectionattempts.WhoProvider, detail: "remote_session_unavailable"},
		{name: "misconfigured", upstream: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"unauthorized_client"}`))
		}, want: mcpconnectionattempts.CodeCredentialMisconfigured, who: mcpconnectionattempts.WhoAdmin, detail: "remote_session_misconfigured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx := requireProjectAuthContext(t, ctx)
			rec := mcpconnectionattemptstest.New(t)
			ti.service.SetConnectionRecorder(rec.Recorder)
			fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-"+tc.name)

			subject := urn.NewUserSubject("gate-user-" + uuid.NewString())
			if tc.upstream != nil {
				upsertExpiredRefreshableRemoteSession(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject)
				pointRemoteSessionTokenEndpoint(t, ctx, ti, authCtx, fixture.RemoteSessionClient.ID, tc.upstream)
			}
			bearer := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

			_, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
			require.Error(t, err)

			row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.want)
			require.Equal(t, string(tc.who), row.WhoFixes)
			require.Equal(t, tc.detail, row.SubReason)
			require.Equal(t, subject.String(), row.SubjectURN)
		})
	}
}

func TestInboundRefresh_Refreshed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))

	refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refreshed.err)
	require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultRefreshed)
	require.Equal(t, "refresh_token", row.GrantType)
	require.Equal(t, urn.NewUserSubject("refresh-replay-user").String(), row.SubjectURN)
	require.Equal(t, client.ID.String(), row.UserSessionClientID)
}

func TestInboundRefresh_Expired(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(-time.Hour))

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenExpired)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
}

// A client that authenticated and sent a spent or unknown token gets a row; the limiter bounds it.
func TestInboundRefresh_UnknownTokenRecords(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, client, _, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, "refresh-"+uuid.NewString())
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
	require.Equal(t, client.ID.String(), row.UserSessionClientID)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
}

func TestInboundRefresh_ReplayIsNotAFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))

	for range 2 {
		refreshed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
		require.NoError(t, refreshed.err)
		require.Equal(t, http.StatusOK, refreshed.code, refreshed.body)
	}
	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultRefreshed)
	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultRefreshReplayed)
}

func TestInboundRefresh_UpstreamReconnect(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuer.ID)

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeUpstreamReconnectRequired)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
}

// A malformed request has no token to judge, and leaves without a no_verdict row.
func TestInboundRefresh_NoTokenLeavesNoRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, client, _, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, "")
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)
	rec.RequireNothing()
}

// A proxied server's failure lands on its own row, through the real request path.
func TestProxiedUpstreamFailureRecords(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "endpoint-" + uuid.NewString()
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, slug, "public", issuerID)
	subject := urn.NewUserSubject(mockidp.MockUserID)
	token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: urn.NewUserSessionIssuer(issuerID).String(),
		Issuer:   ti.serverURL.String() + "/x/mcp/" + slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, subject, jti)

	call, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "ping", "arguments": map[string]any{}}})
	require.NoError(t, err)
	_, _ = servePublicHTTP(t, context.Background(), ti, slug, call, token, nil)

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderServerError)
	require.Equal(t, mcpServer.ID.String(), row.ServerID)
	require.Equal(t, string(mcpconnectionattempts.WhoProvider), row.WhoFixes)
}

// Denied mcp:connect on a proxied server is the admin's to grant.
func TestProxiedConnectDeniedRecords(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)

	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(upstream.Close)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "endpoint-" + uuid.NewString()
	mcpServer, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, slug, "private", issuerID)
	subject := urn.NewUserSubject(mockidp.MockUserID)
	token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: urn.NewUserSessionIssuer(issuerID).String(),
		Issuer:   ti.serverURL.String() + "/x/mcp/" + slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, subject, jti)

	_, err = servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), token, nil)
	require.Error(t, err)

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeNoServerAccess)
	require.Equal(t, mcpServer.ID.String(), row.ServerID)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}

// A hosted tool the caller may not use is recorded, and the response stays the denial.
func TestHostedToolDeniedRecords(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	fx := newWorkloadConsentFixture(t, ctx, ti)
	addHTTPTools(t, ctx, ti, fx.toolset.ID, fx.target.ProjectID, fx.orgID, "deploy", "rollback")

	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	selector := authz.NewSelector(authz.ScopeMCPConnect, fx.target.MCPResourceID.String())
	selector[authz.SelectorKeyTool] = "rollback"
	selectors, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: fx.orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()),
		Scope:          string(authz.ScopeMCPConnect),
		Selectors:      selectors,
	})
	require.NoError(t, err)
	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)
	session := seedWorkloadSession(t, ctx, ti, fx, urn.NewWorkloadSubject(issuerID, workloadSessionSubject))
	token := mintSessionBearerExpiringAt(t, ti, fx, session, session.ExpiresAt.Time)
	slug := fx.toolset.McpSlug.String

	requireAdmitted(t, serveMCPResponse(t, ti, slug, makeInitializeBody(), token))
	w := serveMCPResponse(t, ti, slug, makeToolsCallBody("deploy"), token)
	require.Contains(t, w.Body.String(), mcpaccess.ToolPermissionDeniedMessage)

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeToolPermissionDenied)
	require.Equal(t, fx.orgID, row.OrganizationID)
}

func TestGatewayMemberRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	projectID, orgID := *authCtx.ProjectID, authCtx.ActiveOrganizationID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-rows-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, issuerID)
	deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(deny.Close)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-rows", "", []uuid.UUID{issuerID})
	remoteIssuerID := clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member", deny.URL, conv.ToNullUUID(remoteIssuerID), 0)
	subject := createTestUser(t, ctx, ti, "meta-rows-user-"+uuid.NewString())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, issuerID, subject)

	_, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.True(t, isError)
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeInvalidRemoteSession)
	require.Equal(t, meta.ID.String(), row.ServerID)
	require.NotEmpty(t, row.MemberID)
	require.Equal(t, subject.String(), row.SubjectURN)

	insertQualifiedRemoteSessionToken(t, ctx, ti, issuerID, clientID, subject, "token-refused", deny.URL)
	_, isError = metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, "member--ping"))
	require.True(t, isError)
	rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderRejected)
}

// A gateway member that answers writes nothing, through the real request path.
func TestHealthyGatewayMemberRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newRecordingUpstream(t, "ping")
	memberSlug := "member-remote-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "remote member", memberSlug, 1, upstream.url)

	envelope := callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{"name": memberSlug + "--ping", "arguments": map[string]any{}})
	require.NotContains(t, string(envelope["result"]), `"isError":true`)
	rec.RequireNothing()
}

// A row written the production way reaches ClickHouse, so the recorder is really wired to the service.
func TestRecorderWritesRowsToClickHouse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	recorder := mcpconnectionattempts.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, mcpconnectionattemptstest.LogsOn)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = recorder.Shutdown(cleanupCtx)
	})
	ti.service.SetConnectionRecorder(recorder)
	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "gate-wired")
	subject := urn.NewUserSubject("wired-user-" + uuid.NewString())
	bearer := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject)

	_, err = servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.Error(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, qerr := conn.Query(ctx, `SELECT reason FROM mcp_connection_events WHERE subject_urn = $1 AND is_final`, subject.String())
		if !assert.NoError(c, qerr) {
			return
		}
		defer o11y.NoLogDefer(rows.Close)
		var reasons []string
		for rows.Next() {
			var reason string
			assert.NoError(c, rows.Scan(&reason))
			reasons = append(reasons, reason)
		}
		assert.Contains(c, reasons, mcpconnectionattempts.CodeInvalidRemoteSession.String())
	}, 30*time.Second, 200*time.Millisecond)
}

// Denied mcp:connect on a hosted toolset is the admin's to grant.
func TestHostedConnectDeniedRecords(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _ := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, "hosted-denied-"+uuid.NewString()[:8])
	bearer := mintUserSessionBearerForSubject(t, ti, toolset, urn.NewUserSubject(mockidp.MockUserID))

	_, err := servePublicHTTP(t, context.Background(), ti, toolset.McpSlug.String, makeInitializeBody(), bearer, nil)
	require.Error(t, err)

	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeNoServerAccess)
	require.Equal(t, authCtx.ActiveOrganizationID, row.OrganizationID)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}
