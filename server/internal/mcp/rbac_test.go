package mcp_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServePublic_RBAC_PrivateMCP_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-denied-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx)

	err = authzEngine.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceID: toolset.ID.String()})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestServePublic_RBAC_PrivateMCP_DeniedWithUnrelatedGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-unrelated-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeMCPConnect, Selector: authz.NewSelector(authz.ScopeMCPConnect, uuid.NewString())})

	err = authzEngine.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceID: toolset.ID.String()})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestServePublic_RBAC_PrivateMCP_AllowedWithWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-write-implies-connect-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeMCPWrite, Selector: authz.NewSelector(authz.ScopeMCPWrite, toolset.ID.String())})

	err = authzEngine.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceID: toolset.ID.String()})
	require.NoError(t, err)
}

func TestServePublic_RBAC_PrivateMCP_AllowedWithConnectGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-allowed-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeMCPConnect, Selector: authz.NewSelector(authz.ScopeMCPConnect, toolset.ID.String())})

	err = authzEngine.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceID: toolset.ID.String()})
	require.NoError(t, err)
}

func TestServePublic_RBAC_PublicMCP_AllowedWithoutGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetsRepo := toolsets_repo.New(ti.conn)
	toolset := createPublicMCPToolset(t, ctx, toolsetsRepo, authCtx, "rbac-public-"+uuid.NewString()[:8])

	ctx = authztest.WithExactGrants(t, ctx)

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestServePublic_RBAC_ToolLevelGrant_AllowsMatchingTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-tool-allowed-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope: authz.ScopeMCPConnect,
		Selector: authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   toolset.ID.String(),
			"tool":          "allowed_tool",
		},
	})

	// Tool-level check with matching tool name should pass.
	err = authzEngine.Require(ctx, authz.Check{
		Scope:      authz.ScopeMCPConnect,
		ResourceID: toolset.ID.String(),
		Dimensions: map[string]string{"tool": "allowed_tool"},
	})
	require.NoError(t, err)
}

func TestServePublic_RBAC_ToolLevelGrant_DeniesWrongTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-tool-denied-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope: authz.ScopeMCPConnect,
		Selector: authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   toolset.ID.String(),
			"tool":          "allowed_tool",
		},
	})

	// Tool-level check with different tool name should be denied.
	err = authzEngine.Require(ctx, authz.Check{
		Scope:      authz.ScopeMCPConnect,
		ResourceID: toolset.ID.String(),
		Dimensions: map[string]string{"tool": "forbidden_tool"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestServePublic_RBAC_ServerLevelGrant_AllowsAnyTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-server-any-tool-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	// Server-level grant (no tool key) should allow any tool.
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: authz.NewSelector(authz.ScopeMCPConnect, toolset.ID.String()),
	})

	err = authzEngine.Require(ctx, authz.Check{
		Scope:      authz.ScopeMCPConnect,
		ResourceID: toolset.ID.String(),
		Dimensions: map[string]string{"tool": "any_tool_name"},
	})
	require.NoError(t, err)
}

func TestServePublic_RBAC_ToolLevelGrant_DeniesWrongServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-tool-wrong-srv-"+uuid.NewString()[:8])

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(ti.logger, ti.conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope: authz.ScopeMCPConnect,
		Selector: authz.Selector{
			"resource_kind": "mcp",
			"resource_id":   toolset.ID.String(),
			"tool":          "allowed_tool",
		},
	})

	// Same tool name but different server should be denied.
	err = authzEngine.Require(ctx, authz.Check{
		Scope:      authz.ScopeMCPConnect,
		ResourceID: uuid.NewString(),
		Dimensions: map[string]string{"tool": "allowed_tool"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func createPrivateMCPToolset(t *testing.T, ctx context.Context, ti *testInstance, slug string) toolsets_repo.Toolset {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetsRepo := toolsets_repo.New(ti.conn)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Test Private MCP " + slug,
		Slug:                   slug,
		Description:            conv.ToPGText("A test private MCP for RBAC testing"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	return toolset
}
