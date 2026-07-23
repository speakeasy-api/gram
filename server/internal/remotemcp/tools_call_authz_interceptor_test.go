package remotemcp_test

import (
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const (
	testServerID    = "11111111-1111-1111-1111-111111111111"
	testMcpServerID = "33333333-3333-3333-3333-333333333333"
	testProjectID   = "22222222-2222-2222-2222-222222222222"
)

// testServerIdentity pairs the remote_mcp_servers id with a distinct fronting
// mcp_servers id for the telemetry interceptor tests, so assertions can tell
// the two correlation dimensions apart.
var testServerIdentity = proxy.ServerIdentity{
	RemoteMCPServerID:   testServerID,
	TunneledMCPServerID: "",
	McpServerID:         testMcpServerID,
}

func newAuthzEngineForTest(t *testing.T) *authz.Engine {
	t.Helper()
	return authz.NewEngine(testenv.NewLogger(t), nil, nil, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
}

func authzAuthContext(t *testing.T) *contextvalues.AuthContext {
	t.Helper()
	sessionID := "session_xmcp"
	return &contextvalues.AuthContext{
		ActiveOrganizationID: "org_xmcp",
		UserID:               "user_xmcp",
		SessionID:            &sessionID,
		AccountType:          "enterprise",
	}
}

func newToolsCallRequest(toolName string) *proxy.ToolsCallRequest {
	return &proxy.ToolsCallRequest{
		UserRequest: &proxy.UserRequest{},
		Params: &mcp.CallToolParamsRaw{
			Name:      toolName,
			Arguments: nil,
			Meta:      nil,
		},
	}
}

func TestToolsCallAuthzInterceptor_Name(t *testing.T) {
	t.Parallel()

	interceptor := remotemcp.NewToolsCallAuthzInterceptor(newAuthzEngineForTest(t), emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))
	require.Equal(t, "tools-call-authz", interceptor.Name())
}

func TestToolsCallAuthzInterceptor_NilEnginePassesThrough(t *testing.T) {
	t.Parallel()

	// Defensive: a nil engine must not panic and must not reject.
	interceptor := remotemcp.NewToolsCallAuthzInterceptor(nil, emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), newToolsCallRequest("any_tool")))
}

func TestToolsCallAuthzInterceptor_GrantsAllowMatchingTool(t *testing.T) {
	t.Parallel()

	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   testServerID,
			"tool":          "search_tickets",
		}),
	)

	interceptor := remotemcp.NewToolsCallAuthzInterceptor(engine, emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))

	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, newToolsCallRequest("search_tickets")))
}

func TestToolsCallAuthzInterceptor_GrantsRejectNonMatchingTool(t *testing.T) {
	t.Parallel()

	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   testServerID,
			"tool":          "search_tickets",
		}),
	)

	interceptor := remotemcp.NewToolsCallAuthzInterceptor(engine, emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))

	err := interceptor.InterceptToolsCallRequest(ctx, newToolsCallRequest("delete_ticket"))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Equal(t, mcpaccess.ToolPermissionDeniedMessage, oopsErr.Error())
}

func TestToolsCallAuthzInterceptor_NoGrantsRejects(t *testing.T) {
	t.Parallel()

	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	ctx = authztest.WithExactGrants(t, ctx)

	interceptor := remotemcp.NewToolsCallAuthzInterceptor(engine, emptyResolver(), testServerID, testProjectID, testenv.NewLogger(t))

	err := interceptor.InterceptToolsCallRequest(ctx, newToolsCallRequest("any_tool"))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestToolsCallAuthzInterceptor_DispositionScopedGrantMatches(t *testing.T) {
	t.Parallel()

	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	// Grant applies to any read_only tool on the server (no tool key).
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   testServerID,
			"disposition":   "read_only",
		}),
	)

	resolver := fakeToolDispositionResolver{dispositions: map[string]string{
		"list_items":  "read_only",
		"delete_item": "destructive",
	}}
	interceptor := remotemcp.NewToolsCallAuthzInterceptor(engine, resolver, testServerID, testProjectID, testenv.NewLogger(t))

	// read_only tool matches the disposition-scoped grant.
	require.NoError(t, interceptor.InterceptToolsCallRequest(ctx, newToolsCallRequest("list_items")))

	// destructive tool carries a disposition the grant does not cover, and no
	// tool-scoped grant exists, so it is denied.
	err := interceptor.InterceptToolsCallRequest(ctx, newToolsCallRequest("delete_item"))
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestToolsCallAuthzInterceptor_ResolverErrorFailsClosed(t *testing.T) {
	t.Parallel()

	engine := newAuthzEngineForTest(t)
	ctx := contextvalues.SetAuthContext(t.Context(), authzAuthContext(t))
	// A grant that would otherwise allow the call — the resolver error must
	// deny anyway.
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   testServerID,
			"tool":          "search_tickets",
		}),
	)

	resolver := fakeToolDispositionResolver{err: errors.New("metadata store unavailable")}
	interceptor := remotemcp.NewToolsCallAuthzInterceptor(engine, resolver, testServerID, testProjectID, testenv.NewLogger(t))

	// The call is rejected despite the allowing grant: a resolver failure
	// aborts before the authz check rather than proceeding on empty disposition.
	err := interceptor.InterceptToolsCallRequest(ctx, newToolsCallRequest("search_tickets"))
	require.Error(t, err)
}
