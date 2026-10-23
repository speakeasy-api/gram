package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
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

func (e *recordingCodeExecutor) Available() bool { return true }

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
	enableCodeMode(t, ctx, ti)
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

// TestCodeModeRequiresOrganizationEntitlement pins the per-organization
// rollout: with the runtime available, a Code Mode connection in an
// organization without the entitlement is served Progressive discovery and
// never reaches the runtime, and granting the entitlement takes effect at once.
//
// It runs before the parallel tests: they share this test's mock
// organization and cached entitlement, and grant it Code Mode.
func TestCodeModeRequiresOrganizationEntitlement(t *testing.T) { //nolint:paralleltest // Parallel tests grant the shared mock organization Code Mode through a shared Redis cache.
	ctx, ti := newTestMCPService(t)
	setCodeModeEntitlement(t, ctx, ti, false)
	fx := seedOrganizationGateway(t, ctx, ti)
	executor := &recordingCodeExecutor{mu: sync.Mutex{}, scopes: nil}
	mcp.SetTestCodeExecutor(ti.service.Service, executor)
	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx, metamcp.DiscoveryModeCode)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/list", map[string]any{}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"name":"list_servers"`)
	require.NotContains(t, w.Body.String(), `"name":"execute"`)
	_, err = serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "1"}}))
	require.NoError(t, err)
	require.Empty(t, executor.callers(), "an organization without Code Mode never reaches the runtime")

	enableCodeMode(t, ctx, ti)
	w, err = serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/list", map[string]any{}))
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), `"name":"execute"`, "granting the entitlement takes effect without a deploy")
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
	enableCodeMode(t, ctx, ti)
	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx, metamcp.DiscoveryModeCode)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/list", map[string]any{}))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), open.slug+"--")
	require.NotContains(t, w.Body.String(), closed.slug+"--")
}

// TestCodeModeNeverServesAnonymousCallers pins the serving backstop for a
// Code Mode row written outside the API on a gateway without sign-in: it
// lists Progressive tools, refuses execute, and never reaches the runtime,
// whatever bearer or session an anonymous caller sends.
func TestCodeModeNeverServesAnonymousCallers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
	setGatewayMode(t, ctx, ti, gateway.ID, "code_mode")
	executor := &recordingCodeExecutor{mu: sync.Mutex{}, scopes: nil}
	mcp.SetTestCodeExecutor(ti.service.Service, executor)
	enableCodeMode(t, ctx, ti)

	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"name":"execute_tool"`)
	require.NotContains(t, w.Body.String(), `"name":"execute"`)

	execute := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "1"}})
	for _, bearer := range []string{"", uuid.NewString()} {
		w, err := servePublicHTTP(t, t.Context(), ti, slug, execute, bearer, map[string]string{"Mcp-Session-Id": uuid.NewString()})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"error":{"code":-32002,"message":"unknown tool \"execute\""}`, "execute is refused, not run")
	}
	require.Empty(t, executor.callers(), "an anonymous caller never reaches the code runtime")
}

// TestCodeModeRefusalIsStructured pins that an execution refused before
// submission reports a typed, retryable error instead of an opaque message.
func TestCodeModeRefusalIsStructured(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	mcp.SetTestCodeExecutor(ti.service.Service, &refusingCodeExecutor{err: fmt.Errorf("admit: %w", codemode.ErrCapacity)})
	enableCodeMode(t, ctx, ti)
	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx, metamcp.DiscoveryModeCode)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "1"}}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Error struct {
					Code         string `json:"code"`
					Message      string `json:"message"`
					Retryable    bool   `json:"retryable"`
					RetryAfterMS int    `json:"retry_after_ms"`
				} `json:"error"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.True(t, response.Result.IsError)
	refusal := response.Result.StructuredContent.Error
	require.Equal(t, "capacity_exceeded", refusal.Code)
	require.True(t, refusal.Retryable)
	require.Positive(t, refusal.RetryAfterMS)
	require.Contains(t, refusal.Message, "No program was submitted.")
	var timed struct {
		Result struct {
			Meta map[string]json.RawMessage `json:"_meta"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &timed))
	var refusalTiming map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(timed.Result.Meta["com.speakeasy.ai/timing"], &refusalTiming))
	require.Contains(t, refusalTiming, "server_ms")
	require.NotContains(t, refusalTiming, "admission_ms", "a refusal reports server time only")
	require.NotContains(t, refusalTiming, "run_ms")
	require.Contains(t, timed.Result.Meta, "io.modelcontextprotocol/serverInfo", "timing joins the server identity in _meta")
}

// TestCodeModeReportsServerTiming pins the timing an execute result carries
// in _meta, so clients can separate server time from their own network time.
func TestCodeModeReportsServerTiming(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	executor := &recordingCodeExecutor{mu: sync.Mutex{}, scopes: nil, run: func(context.Context, string, codemode.Host) (*codemode.ExecutionResult, error) {
		return &codemode.ExecutionResult{Value: json.RawMessage("2"), ToolCalls: []codemode.CallOutcome{}, Timing: codemode.ExecutionTiming{Admission: 3 * time.Millisecond, Run: 12 * time.Millisecond}}, nil
	}}
	mcp.SetTestCodeExecutor(ti.service.Service, executor)
	enableCodeMode(t, ctx, ti)
	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx, metamcp.DiscoveryModeCode)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "1+1"}}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var response struct {
		Result struct {
			Meta map[string]struct {
				ServerMS    int64  `json:"server_ms"`
				AdmissionMS *int64 `json:"admission_ms"`
				RunMS       *int64 `json:"run_ms"`
			} `json:"_meta"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	timing, ok := response.Result.Meta["com.speakeasy.ai/timing"]
	require.True(t, ok, w.Body.String())
	require.NotNil(t, timing.AdmissionMS)
	require.NotNil(t, timing.RunMS)
	require.Equal(t, int64(3), *timing.AdmissionMS)
	require.Equal(t, int64(12), *timing.RunMS)
	require.GreaterOrEqual(t, timing.ServerMS, int64(0))
}

// TestCodeModeRunnerRefusalIsStructured pins that a runner refusing a
// program before it starts reports the same structured shape as admission.
func TestCodeModeRunnerRefusalIsStructured(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedOrganizationGateway(t, ctx, ti)
	executor := &recordingCodeExecutor{mu: sync.Mutex{}, scopes: nil, run: func(context.Context, string, codemode.Host) (*codemode.ExecutionResult, error) {
		return &codemode.ExecutionResult{Value: json.RawMessage("null"), ToolCalls: []codemode.CallOutcome{}, Error: &codemode.ExecutionError{Code: "capacity_exceeded", Message: "full"}}, nil
	}}
	mcp.SetTestCodeExecutor(ti.service.Service, executor)
	enableCodeMode(t, ctx, ti)
	bearer := mintOrganizationGatewayBearerWithPolicy(t, ctx, ti, fx, metamcp.DiscoveryModeCode)

	w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{"name": "execute", "arguments": map[string]any{"code": "1"}}))
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), `"code":"capacity_exceeded"`)
	require.Contains(t, w.Body.String(), `"retryable":true`)
	require.Contains(t, w.Body.String(), `"retry_after_ms":1000`)
}

// refusingCodeExecutor refuses every execution before submission.
type refusingCodeExecutor struct{ err error }

func (*refusingCodeExecutor) Enabled() bool   { return true }
func (*refusingCodeExecutor) Available() bool { return true }
func (e *refusingCodeExecutor) Execute(context.Context, codemode.Scope, string, codemode.HostFactory) (*codemode.ExecutionResult, error) {
	return nil, e.err
}
func (*refusingCodeExecutor) Cancel(context.Context, codemode.Scope) error { return nil }

func TestCodeModeDescriptionOmitsMembersWithNoPermittedTools(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	mcp.SetTestCodeExecutor(ti.service.Service, &recordingCodeExecutor{mu: sync.Mutex{}, scopes: nil})
	enableCodeMode(t, ctx, ti)
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
