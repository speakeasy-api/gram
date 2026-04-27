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

	authzEngine := authz.NewEngine(ti.logger, ti.conn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx)

	err := authzEngine.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceID: toolset.ID.String()})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestServePublic_RBAC_PrivateMCP_DeniedWithUnrelatedGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-unrelated-"+uuid.NewString()[:8])

	authzEngine := authz.NewEngine(ti.logger, ti.conn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPConnect, uuid.NewString()))

	err := authzEngine.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceID: toolset.ID.String()})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestServePublic_RBAC_PrivateMCP_AllowedWithWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-write-implies-connect-"+uuid.NewString()[:8])

	authzEngine := authz.NewEngine(ti.logger, ti.conn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPWrite, toolset.ID.String()))

	err := authzEngine.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceID: toolset.ID.String()})
	require.NoError(t, err)
}

func TestServePublic_RBAC_PrivateMCP_AllowedWithConnectGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset := createPrivateMCPToolset(t, ctx, ti, "rbac-allowed-"+uuid.NewString()[:8])

	authzEngine := authz.NewEngine(ti.logger, ti.conn, authztest.RBACAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPConnect, toolset.ID.String()))

	err := authzEngine.Require(ctx, authz.Check{Scope: authz.ScopeMCPConnect, ResourceID: toolset.ID.String()})
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
