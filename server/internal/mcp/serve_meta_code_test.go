package mcp_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/stretchr/testify/require"
)

func TestCodeModeRefusesMissingRuntime(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	auth, _ := contextvalues.GetAuthContext(ctx)
	slug := "code-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, uuid.Nil)
	setGatewayMode(t, ctx, ti, gateway.ID, "code_mode")
	_, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.ErrorContains(t, err, "code mode runtime is unavailable")
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
