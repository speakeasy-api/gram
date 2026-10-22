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
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
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
	token := mintGatewayTestSession(t, ctx, ti, gated.ID, issuer, gatedSlug, metamcp.DiscoveryModeCode)
	requireProgressive(t, gatedSlug, token)
}

func TestCodeModeUnknownStoredModeFallsBackToProgressive(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
	setGatewayMode(t, ctx, ti, gateway.ID, "future_mode")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"name":"execute_tool"`)
	require.NotContains(t, w.Body.String(), `"name":"execute"`)
}

// recordingCodeExecutor records admitted scopes and optionally exercises the host callbacks.
type recordingCodeExecutor struct {
	mu     sync.Mutex
	scopes []codemode.Scope
	run    func(context.Context, string, codemode.Host) (*codemode.ExecutionResult, error)
}

func (e *recordingCodeExecutor) Enabled() bool { return true }

func (e *recordingCodeExecutor) Execute(ctx context.Context, scope codemode.Scope, code string, factory codemode.HostFactory) (*codemode.ExecutionResult, error) {
	e.mu.Lock()
	e.scopes = append(e.scopes, scope)
	e.mu.Unlock()
	if e.run != nil {
		host, err := factory(ctx)
		if err != nil {
			return nil, err
		}
		return e.run(ctx, code, host)
	}
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

func TestCodeModeOrganizationGatewayRunsOrganizationWide(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	executor := &recordingCodeExecutor{mu: sync.Mutex{}, scopes: nil}
	mcp.SetTestCodeExecutor(ti.service.Service, executor)
	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx, metamcp.DiscoveryModeCode)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/list", map[string]any{}))
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), `"name":"execute"`)
	require.NotContains(t, w.Body.String(), `"name":"list_servers"`)

	w, err = serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "1"}}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	executor.mu.Lock()
	defer executor.mu.Unlock()
	require.Len(t, executor.scopes, 1)
	require.True(t, executor.scopes[0].OrganizationWide)
	require.Equal(t, uuid.Nil, executor.scopes[0].Project)
	require.NotEqual(t, uuid.Nil, executor.scopes[0].Gateway)
}

// An organization gateway's Code Mode catalog omits a hosted member whose
// private toolset the caller cannot connect to: derived membership drops it
// (derivedMemberAuth), so no prefix is advertised whose every call would be
// refused at dispatch.
func TestCodeModeOrganizationGatewayHidesUnconnectableToolsets(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	decoy := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	open := seedHostedMetaMember(t, ctx, ti, decoy.ID, "open hosted", 0, mcpservers.VisibilityPublic, "open_tool")
	closed := seedHostedMetaMember(t, ctx, ti, decoy.ID, "closed hosted", 1, mcpservers.VisibilityPublic, "closed_tool")
	setToolsetMcpPrivate(t, ctx, ti, closed.toolsetID, *auth.ProjectID)
	mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{mu: sync.Mutex{}, scopes: nil})
	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx, metamcp.DiscoveryModeCode)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/list", map[string]any{}))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), open.slug+"--")
	require.NotContains(t, w.Body.String(), closed.slug+"--")
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
	mcp.SetTestCodeExecutor(ti.service.Service, executor)
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
	mcp.SetTestCodeExecutor(ti.service.Service, executor)
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
	mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{mu: sync.Mutex{}, scopes: nil})
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	upstream := newRecordingUpstream(t, "ping")
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Approved tools", "approved", 0, upstream.url)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *auth.ProjectID, gateway.ID, "Other tools", "other", 1, upstream.url)

	list := func(token string) string {
		t.Helper()
		w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
		require.Contains(t, w.Body.String(), `"name":"execute"`)
		return w.Body.String()
	}
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeCode, []string{"approved--ping"})
	body := list(connection.token)
	require.Contains(t, body, `approved--`)
	require.NotContains(t, body, `other--`)

	body = list(mintGatewayTestSession(t, ctx, ti, gateway.ID, issuer.ID, slug, metamcp.DiscoveryModeCode))
	require.Contains(t, body, `approved--`)
	require.Contains(t, body, `other--`)
}

// mintGatewayTestSession stores a gateway session with an explicit discovery
// mode and the principal grants of a catalogue-less session.
func mintGatewayTestSession(t *testing.T, ctx context.Context, ti *testInstance, gatewayID, issuerID uuid.UUID, slug string, mode metamcp.DiscoveryMode) string {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	policy, err := json.Marshal(&toolfilter.SessionPolicy{Resource: "meta_mcp_server:" + gatewayID.String(), Selection: nil, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &mode}})
	require.NoError(t, err)
	subject := urn.NewUserSubject(authCtx.UserID)
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{Subject: subject, Audience: urn.NewUserSessionIssuer(issuerID).String(), Issuer: ti.serverURL.JoinPath("mcp", slug).String(), Lifetime: time.Hour})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: issuerID, SubjectUrn: subject, Jti: jti, RefreshTokenHash: conv.ToPGText(uuid.NewString()), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ToolSelection: policy})
	require.NoError(t, err)
	return token
}
