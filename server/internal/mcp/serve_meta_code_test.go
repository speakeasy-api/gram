package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

// TestCodeModeWithoutRuntimeServesProgressive pins the kill switch: with no
// runtime, a code_mode gateway and a connection that chose code_mode at
// consent both keep working in progressive mode.
func TestCodeModeWithoutRuntimeServesProgressive(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
	setGatewayMode(t, ctx, ti, gateway.ID, "code_mode")
	requireProgressive := func(t *testing.T, slug, token string) {
		t.Helper()
		for _, method := range []string{"initialize", "tools/list"} {
			params := map[string]any{}
			if method == "initialize" {
				params = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}}
			}
			w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, method, params), token, nil)
			require.NoError(t, err, method)
			require.Equal(t, http.StatusOK, w.Code, method)
			require.NotContains(t, w.Body.String(), "Code Mode", method)
			if method == "tools/list" {
				require.Contains(t, w.Body.String(), `"name":"execute_tool"`)
			}
		}
	}
	requireProgressive(t, slug, "")

	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	gatedSlug := "code-" + uuid.NewString()
	gated := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, gatedSlug, issuer)
	token := mintGatewayTestSession(t, ctx, ti, gated.ID, issuer, gatedSlug, metamcp.DiscoveryModeCode, nil, nil)
	requireProgressive(t, gatedSlug, token)
}

func TestCodeModeUnknownStoredModeFailsClosed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
	setGatewayMode(t, ctx, ti, gateway.ID, "future_mode")
	_, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.ErrorContains(t, err, "unsupported gateway discovery mode")
	setGatewayMode(t, ctx, ti, gateway.ID, "progressive")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
}

// recordingCodeExecutor admits every execution without running it and keeps
// the scopes it was asked to run.
type recordingCodeExecutor struct {
	mu     sync.Mutex
	scopes []codemode.Scope
}

func (e *recordingCodeExecutor) Enabled() bool { return true }

func (e *recordingCodeExecutor) Execute(_ context.Context, scope codemode.Scope, _ string, _ codemode.HostFactory) (*codemode.ExecutionResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.scopes = append(e.scopes, scope)
	return &codemode.ExecutionResult{}, nil
}

func (e *recordingCodeExecutor) Cancel(context.Context, codemode.Scope) error { return nil }

func (e *recordingCodeExecutor) callers() map[string]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	callers := map[string]bool{}
	for _, scope := range e.scopes {
		callers[strings.Join(scope.Caller, "\x00")] = true
	}
	return callers
}

// TestCodeModeAnonymousCallersNeedSessionAndShareBoundedRunners pins the
// anonymous admission rules: a caller without an MCP session is refused
// before any runner is requested, and any number of anonymous sessions maps
// onto a bounded set of runner identities per gateway.
func TestCodeModeAnonymousCallersNeedSessionAndShareBoundedRunners(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
	setGatewayMode(t, ctx, ti, gateway.ID, "code_mode")
	executor := &recordingCodeExecutor{mu: sync.Mutex{}, scopes: nil}
	mcp.SetTestCodeExecutor(ti.service, executor)
	execute := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "1"}})

	w, err := servePublicHTTP(t, t.Context(), ti, slug, execute, "", nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), "Mcp-Session-Id")
	require.Empty(t, executor.callers(), "a sessionless anonymous caller must not reach the runtime")

	for range 32 {
		_, err := servePublicHTTP(t, t.Context(), ti, slug, execute, "", map[string]string{"Mcp-Session-Id": uuid.NewString()})
		require.NoError(t, err)
	}
	callers := executor.callers()
	require.NotEmpty(t, callers)
	require.LessOrEqual(t, len(callers), 4, "anonymous sessions share a bounded set of runners")
}

// TestCodeModeUnverifiedBearerStaysAnonymous pins that an ungated gateway,
// which never checks the Authorization header, applies the anonymous rules
// to a caller that sends one: no session is refused, and a fresh bearer per
// request cannot claim a fresh runner.
func TestCodeModeUnverifiedBearerStaysAnonymous(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
	setGatewayMode(t, ctx, ti, gateway.ID, "code_mode")
	executor := &recordingCodeExecutor{mu: sync.Mutex{}, scopes: nil}
	mcp.SetTestCodeExecutor(ti.service, executor)
	execute := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "1"}})

	w, err := servePublicHTTP(t, t.Context(), ti, slug, execute, uuid.NewString(), nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), "Mcp-Session-Id")
	require.Empty(t, executor.callers(), "an unverified bearer must not stand in for an MCP session")

	for range 32 {
		_, err := servePublicHTTP(t, t.Context(), ti, slug, execute, uuid.NewString(), map[string]string{"Mcp-Session-Id": uuid.NewString()})
		require.NoError(t, err)
	}
	callers := executor.callers()
	require.NotEmpty(t, callers)
	require.LessOrEqual(t, len(callers), 4, "unverified bearers share the anonymous runners")
}

func TestCodeModeDescriptionOmitsMembersWithNoPermittedTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	mcp.SetTestCodeExecutor(ti.service, &recordingCodeExecutor{mu: sync.Mutex{}, scopes: nil})
	auth, _ := contextvalues.GetAuthContext(ctx)
	issuer := createUserSessionIssuer(t, ctx, ti.conn, *auth.ProjectID)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer)
	upstream := newRecordingUpstream(t, "ping")
	approvedID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Approved tools", "approved", 0, upstream.url)
	otherID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Other tools", "other", 1, upstream.url)
	selection, err := toolfilter.NewSessionSelection("meta_mcp_server:"+gateway.ID.String(), uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeTool, Name: "approved--ping", Mode: nil, Tools: nil},
	})
	require.NoError(t, err)
	members := map[string]uuid.UUID{"approved": approvedID, "other": otherID}

	list := func(token string) string {
		t.Helper()
		w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
		require.Contains(t, w.Body.String(), `"name":"execute"`)
		return w.Body.String()
	}
	body := list(mintGatewayTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode, selection, members))
	require.Contains(t, body, `approved--`)
	require.NotContains(t, body, `other--`)

	body = list(mintGatewayTestSession(t, ctx, ti, gateway.ID, issuer, slug, metamcp.DiscoveryModeCode, nil, nil))
	require.Contains(t, body, `approved--`)
	require.Contains(t, body, `other--`)
}

// mintGatewayTestSession stores a gateway session with an explicit discovery
// mode and an optional tool selection, returning its bearer token.
func mintGatewayTestSession(t *testing.T, ctx context.Context, ti *testInstance, gatewayID, issuerID uuid.UUID, slug string, mode metamcp.DiscoveryMode, selection *toolfilter.SessionSelection, members map[string]uuid.UUID) string {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	policy, err := json.Marshal(&toolfilter.SessionPolicy{Resource: "meta_mcp_server:" + gatewayID.String(), Selection: selection, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &mode}, Members: members})
	require.NoError(t, err)
	subject := urn.NewUserSubject(authCtx.UserID)
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{Subject: subject, Audience: urn.NewUserSessionIssuer(issuerID).String(), Issuer: ti.serverURL.JoinPath("mcp", slug).String(), Lifetime: time.Hour})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: issuerID, SubjectUrn: subject, Jti: jti, RefreshTokenHash: conv.ToPGText(uuid.NewString()), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ToolSelection: policy})
	require.NoError(t, err)
	return token
}
