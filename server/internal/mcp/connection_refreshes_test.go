package mcp_test

import (
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

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

	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"refresh-" + uuid.NewString()}, "client_id": {client.ClientID}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "claude-code/2.1.0")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleToken(w, req))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed)
	require.Equal(t, client.ID.String(), row.UserSessionClientID)
	require.Equal(t, mcpconnectionattempts.FamilyClaudeCode, row.ClientFamily)
	require.Equal(t, "2.1.0", row.ClientVersion)
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
