package mcp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	servedPing        = `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	servedUnknownCall = `{"jsonrpc":"2.0","id":1,"method":"no/such_method","params":{}}`
)

// hostedServedCaller is a fresh named caller on its own issuer-gated hosted toolset, so its window is open.
func hostedServedCaller(t *testing.T, ctx context.Context, ti *testInstance, upstream string) (slug, bearer string, subject urn.SessionSubject) {
	t.Helper()
	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createIssuerGatedHTTPToolFixture(t, ctx, ti, authCtx, "served-"+uuid.NewString()[:8], upstream)
	subject = urn.NewUserSubject("served-user-" + uuid.NewString())
	insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject, "valid-upstream-token", time.Now().Add(time.Hour))
	return fixture.Toolset.McpSlug.String, mintUserSessionBearerForSubject(t, ti, fixture.Toolset, subject), subject
}

func servedRows(rec *mcpconnectionattemptstest.Recording) []mcpconnectionattempts.Row {
	var rows []mcpconnectionattempts.Row
	for _, row := range rec.Rows() {
		if row.StepResult == mcpconnectionattempts.ResultServed.String() {
			rows = append(rows, row)
		}
	}
	return rows
}

// A served row means the caller got a successful result: a JSON-RPC error or an isError tool
// result writes none, on a hosted server and on a gateway.
func TestServedRowNeedsASuccessfulResult(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	working := newAuthorizationRecorder(t, new(atomic.Value))
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)

	slug, bearer, _ := hostedServedCaller(t, ctx, ti, working.URL)
	resp, err := servePublicHTTP(t, context.Background(), ti, slug, []byte(servedUnknownCall), bearer, nil)
	require.NoError(t, err)
	require.Contains(t, resp.Body.String(), `"error"`)
	require.Empty(t, servedRows(rec), "a JSON-RPC error is not a success")

	authCtx := requireProjectAuthContext(t, ctx)
	fixture := createIssuerGatedHTTPToolFixture(t, ctx, ti, authCtx, "served-err-"+uuid.NewString()[:8], failing.URL)
	failingSubject := urn.NewUserSubject("served-err-user-" + uuid.NewString())
	insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, failingSubject, "valid-upstream-token", time.Now().Add(time.Hour))
	call := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"` + fixture.ToolName + `","arguments":{}}}`
	resp, err = servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, []byte(call), mintUserSessionBearerForSubject(t, ti, fixture.Toolset, failingSubject), nil)
	require.NoError(t, err)
	require.Contains(t, resp.Body.String(), `"isError":true`)
	require.Empty(t, servedRows(rec), "an isError tool result is not a success")

	gatewayError := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, gatewayError, uuid.Nil)
	resp, err = servePublicHTTP(t, ctx, ti, gatewayError, []byte(servedUnknownCall), "", nil)
	require.NoError(t, err)
	require.Contains(t, resp.Body.String(), `"error"`)

	member := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: []testmcp.Tool{{
		Name:        "ping",
		Description: "fails",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Response:    testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "tool failed"}}, IsError: true},
	}}})
	t.Cleanup(member.Close)
	memberSlug := "member-error-" + uuid.NewString()[:8]
	gatewayToolError := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, gatewayToolError, uuid.Nil)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "erroring member", memberSlug, 1, member.URL)
	envelope := callMetaTool(t, ctx, ti, gatewayToolError, "execute_tool", map[string]any{"name": memberSlug + "--ping", "arguments": map[string]any{}})
	require.Contains(t, string(envelope["result"]), `"isError":true`)
	require.Empty(t, servedRows(rec), "a member's isError result writes no row for the member or the gateway")

	slug, bearer, subject := hostedServedCaller(t, ctx, ti, working.URL)
	resp, err = servePublicHTTP(t, context.Background(), ti, slug, []byte(servedPing), bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	gatewayPing := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, gatewayPing, uuid.Nil)
	resp, err = servePublicHTTP(t, ctx, ti, gatewayPing, []byte(servedPing), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	rows := servedRows(rec)
	require.Len(t, rows, 2, "a plain ping is a success, hosted and through a gateway")
	require.Equal(t, subject.String(), rows[0].SubjectURN)
}

type failingResponseWriter struct {
	*httptest.ResponseRecorder
}

var errClientGone = errors.New("client gone")

func (failingResponseWriter) Write([]byte) (int, error) { return 0, errClientGone }

func servePublicTo(t *testing.T, ctx context.Context, ti *testInstance, slug, body, bearer string, w http.ResponseWriter) error {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug, strings.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	return ti.service.ServePublic(w, req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx)))
}

// A response that never reached the caller writes no row and claims nothing, hosted or through a gateway.
func TestServedRowNeedsADeliveredResponse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	authCtx := requireProjectAuthContext(t, ctx)
	slug, bearer, _ := hostedServedCaller(t, ctx, ti, newAuthorizationRecorder(t, new(atomic.Value)).URL)
	gateway := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, gateway, uuid.Nil)

	require.Error(t, servePublicTo(t, context.Background(), ti, slug, servedPing, bearer, failingResponseWriter{httptest.NewRecorder()}))
	require.Error(t, servePublicTo(t, ctx, ti, gateway, servedPing, "", failingResponseWriter{httptest.NewRecorder()}))
	rec.RequireNothing()

	require.NoError(t, servePublicTo(t, context.Background(), ti, slug, servedPing, bearer, httptest.NewRecorder()))
	require.NoError(t, servePublicTo(t, ctx, ti, gateway, servedPing, "", httptest.NewRecorder()))
	require.Len(t, servedRows(rec), 2, "the failed writes left both windows open")
}

// A tools/call with no id is owed no answer, so a working member's call writes no row.
func TestIDLessMemberCallRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	authCtx := requireProjectAuthContext(t, ctx)
	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newRecordingUpstream(t, "ping")
	memberSlug := "member-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "member", memberSlug, 1, upstream.url)

	body := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"execute_tool","arguments":{"name":"` + memberSlug + `--ping","arguments":{}}}}`
	_, err := servePublicHTTP(t, ctx, ti, slug, []byte(body), "", nil)
	require.NoError(t, err)
	select {
	case <-upstream.toolCallServed:
	case <-time.After(5 * time.Second):
		t.Log("the id-less call did not reach the member")
	}
	rec.RequireNothing()
}
