// Asserts the issuer-gated /mcp/{slug} resolver accepts an assistant
// JWT and forwards the owning user's remote_session access token to the
// upstream MCP server.
package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestServePublic_AssistantTokenResolvesOwnerRemoteSessionToUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mockTools := []testmcp.Tool{{
		Name:        "ping",
		Description: "Returns pong",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Response: testmcp.ToolResponse{
			Content: []map[string]any{{"type": "text", "text": "pong"}},
		},
	}}
	mockServer := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, mockTools)
	t.Cleanup(mockServer.Close)

	var capturedAuth atomic.Value
	target, err := url.Parse(mockServer.URL)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	recorder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			capturedAuth.Store(auth)
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(recorder.Close)

	fixture := createIssuerGatedExternalMCPFixture(t, ctx, ti, authCtx, "assistant-resolver", recorder.URL)

	ownerSubject := urn.NewUserSubject(authCtx.UserID)
	insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, ownerSubject, "valid-upstream-token", time.Now().Add(time.Hour))

	assistantToken := mintAssistantBearerForOwner(t, ti, authCtx)

	initBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "assistant-runtime", "version": "1.0.0"},
		},
	})
	require.NoError(t, err)
	initResp, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, initBody, assistantToken, nil)
	require.NoError(t, err, "initialize must succeed via the assistant-token fallback")
	require.Equal(t, http.StatusOK, initResp.Code, "initialize response: %s", initResp.Body.String())

	callBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      fixture.ToolName,
			"arguments": map[string]any{},
		},
	})
	require.NoError(t, err)
	callResp, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, callBody, assistantToken, nil)
	require.NoError(t, err, "tools/call must succeed via the assistant-token fallback")
	require.Equal(t, http.StatusOK, callResp.Code, "tools/call response: %s", callResp.Body.String())

	got, _ := capturedAuth.Load().(string)
	require.Equal(t, "Bearer valid-upstream-token", got,
		"resolver must forward the owner's exchanged remote_session token even when the caller authenticated with an assistant JWT")
}

func TestServePublic_AssistantTokenWithoutRemoteSessionChallenges(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "assistant-resolver-no-session")

	assistantToken := mintAssistantBearerForOwner(t, ti, authCtx)
	w, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), assistantToken, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
	require.Contains(t, w.Header().Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource/mcp/"+fixture.Toolset.McpSlug.String,
		"assistant-token fallback must still 401 when the owner has no remote_session for the issuer")
}

func mintAssistantBearerForOwner(
	t *testing.T,
	ti *testInstance,
	authCtx *contextvalues.AuthContext,
) string {
	t.Helper()

	ctx := t.Context()

	assistant, err := assistantsrepo.New(ti.conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:       *authCtx.ProjectID,
		OrganizationID:  authCtx.ActiveOrganizationID,
		CreatedByUserID: pgtype.Text{String: authCtx.UserID, Valid: true},
		Name:            "Resolver Test Assistant " + uuid.NewString()[:8],
		Model:           "openai/gpt-4o-mini",
		Instructions:    "",
		WarmTtlSeconds:  300,
		MaxConcurrency:  1,
		Status:          assistants.StatusActive,
	})
	require.NoError(t, err)

	token, err := assistanttokens.New("test-jwt-secret", ti.conn, ti.authzEngine).Generate(assistanttokens.GenerateInput{
		OrgID:       authCtx.ActiveOrganizationID,
		ProjectID:   *authCtx.ProjectID,
		UserID:      authCtx.UserID,
		AssistantID: assistant.ID,
		ThreadID:    uuid.Nil,
		TTL:         time.Hour,
	})
	require.NoError(t, err)
	return token
}
