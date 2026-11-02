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
	"github.com/stretchr/testify/require"

	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
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

	subject := urn.NewUserSubject("rejected-user")
	token, _, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: "urn:some-other-server",
		Issuer:   ti.serverURL.String() + "/mcp/" + fixture.Toolset.McpSlug.String,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
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

func TestInboundRefresh_Unknown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, client, _, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))

	refused := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, "refresh-"+uuid.NewString())
	require.NoError(t, refused.err)
	require.Equal(t, http.StatusBadRequest, refused.code, refused.body)
	rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
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
