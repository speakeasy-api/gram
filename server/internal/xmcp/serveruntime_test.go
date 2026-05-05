package xmcp_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}`

// runHandlerWithHeaders is a generalized variant of runHandler that threads
// extra request headers (e.g. Mcp-Session-Id) onto the test request.
func runHandlerWithHeaders(t *testing.T, ctx context.Context, ti *testInstance, method, serverID, authorization string, body []byte, extraHeaders map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	mux := chi.NewMux()
	mux.MethodFunc(method, xmcp.RuntimePath, oops.ErrHandle(ti.logger, ti.service.ServeMCP).ServeHTTP)

	req := httptest.NewRequestWithContext(ctx, method, "/x/mcp/"+serverID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// insertProject creates a stub project row so we can test cross-project
// isolation and returns its id for use as a foreign key on remote_mcp_servers.
func insertProject(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) uuid.UUID {
	t.Helper()

	slug := "other-project-" + uuid.NewString()[:8]
	p, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	return p.ID
}

func TestServeRuntime_MissingAuthReturns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	rr := runHandler(t, ctx, ti, http.MethodPost, uuid.NewString(), "", []byte(initializeBody))
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestServeRuntime_InvalidAuthReturns401(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	rr := runHandler(t, ctx, ti, http.MethodPost, uuid.NewString(), bearer("gram_test_not_a_real_key"), []byte(initializeBody))
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestServeRuntime_InvalidServerIDReturns400(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, authCtx.ProjectID, []string{auth.APIKeyScopeConsumer.String()})

	rr := runHandler(t, ctx, ti, http.MethodPost, "not-a-uuid", bearer(key), []byte(initializeBody))
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestServeRuntime_NonExistentServerReturns404(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, authCtx.ProjectID, []string{auth.APIKeyScopeConsumer.String()})

	rr := runHandler(t, ctx, ti, http.MethodPost, uuid.NewString(), bearer(key), []byte(initializeBody))
	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestServeRuntime_Post_ForwardsToRemoteMCPServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{
		Tools: []testmcp.Tool{{
			Name:        "get_weather",
			Description: "Get current weather for a location",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{"type": "string"},
				},
				"required": []any{"location"},
			},
			Response: testmcp.ToolResponse{
				Content: []map[string]any{
					{"type": "text", "text": "San Francisco: sunny, 72F"},
				},
			},
		}},
	})
	t.Cleanup(mockServer.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID
	require.NotNil(t, projectID)

	server := seedRemoteMCPServer(t, ctx, ti, *projectID, mockServer.URL)
	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, projectID, []string{auth.APIKeyScopeConsumer.String()})

	initResp := runHandler(t, ctx, ti, http.MethodPost, server.ID.String(), bearer(key), []byte(initializeBody))
	require.Equal(t, http.StatusOK, initResp.Code, "initialize body=%s", initResp.Body.String())

	// The MCP SDK allocates a session id on initialize and returns it in the
	// Mcp-Session-Id header. The proxy must relay that back to the caller so
	// subsequent requests can be bound to the same session.
	sessionID := initResp.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID, "proxy must relay Mcp-Session-Id from upstream")
}

func TestServeRuntime_Post_ProjectIsolationReturns404(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	mockServer := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: nil})
	t.Cleanup(mockServer.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherProjectID := insertProject(t, ctx, ti, authCtx.ActiveOrganizationID)

	// Server belongs to a different project than the API key's.
	server := seedRemoteMCPServer(t, ctx, ti, otherProjectID, mockServer.URL)
	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, authCtx.ProjectID, []string{auth.APIKeyScopeConsumer.String()})

	rr := runHandler(t, ctx, ti, http.MethodPost, server.ID.String(), bearer(key), []byte(initializeBody))
	require.Equal(t, http.StatusNotFound, rr.Code, "servers in other projects must not be reachable")
}

func TestServeRuntime_Post_AppliesStaticSecretHeader(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var gotAPIKey string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-Upstream-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	server := seedRemoteMCPServer(t, ctx, ti, *authCtx.ProjectID, upstream.URL, remotemcprepo.CreateHeaderParams{
		Name:       "X-Upstream-Api-Key",
		Value:      pgtype.Text{String: "upstream-secret", Valid: true},
		IsRequired: true,
		IsSecret:   true,
	})

	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, authCtx.ProjectID, []string{auth.APIKeyScopeConsumer.String()})

	rr := runHandler(t, ctx, ti, http.MethodPost, server.ID.String(), bearer(key), []byte(initializeBody))
	<-done
	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "upstream-secret", gotAPIKey, "secret static header must be decrypted and forwarded")
}

func TestServeRuntime_Delete_ForwardsSessionTermination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var gotMethod, gotSession string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotSession = r.Header.Get("Mcp-Session-Id")
		w.WriteHeader(http.StatusNoContent)
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	server := seedRemoteMCPServer(t, ctx, ti, *authCtx.ProjectID, upstream.URL)
	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, authCtx.ProjectID, []string{auth.APIKeyScopeConsumer.String()})

	rr := runHandlerWithHeaders(t, ctx, ti, http.MethodDelete, server.ID.String(), bearer(key), nil, map[string]string{"Mcp-Session-Id": "abc-session"})
	<-done
	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Equal(t, http.MethodDelete, gotMethod)
	require.Equal(t, "abc-session", gotSession)
}

func TestServeRuntime_StripsAuthorizationHeaderFromUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var gotAuth string
	done := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		done <- struct{}{}
	}))
	t.Cleanup(upstream.Close)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	server := seedRemoteMCPServer(t, ctx, ti, *authCtx.ProjectID, upstream.URL)
	key := seedAPIKey(t, ctx, ti, authCtx.ActiveOrganizationID, authCtx.UserID, authCtx.ProjectID, []string{auth.APIKeyScopeConsumer.String()})

	rr := runHandler(t, ctx, ti, http.MethodPost, server.ID.String(), bearer(key), []byte(initializeBody))
	<-done
	require.Equal(t, http.StatusOK, rr.Code)
	require.Empty(t, gotAuth, "Gram API key must never leak to the remote MCP server")
}
