package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestToolsets_RBAC_List_ReturnsEmptyWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	_ = createMinimalPrivateToolset(t, ctx, ti, "rbac-list-empty-test")

	ctx = authztest.WithExactGrants(t, ctx)

	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Toolsets)
}

func TestToolsets_RBAC_List_FiltersToGrantedToolsets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "rbac-filter-test")

	ctx = authztest.WithExactGrants(t, ctx, access.NewGrant(access.ScopeMCPRead, toolset.ID))

	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Toolsets, 1)
	require.Equal(t, toolset.ID, result.Toolsets[0].ID)
}

func TestToolsets_RBAC_List_ReturnsEmptyWithWrongResourceGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	_ = createMinimalPrivateToolset(t, ctx, ti, "rbac-excluded-test")

	ctx = authztest.WithExactGrants(t, ctx, access.NewGrant(access.ScopeMCPRead, uuid.NewString()))

	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Toolsets)
}

func TestToolsets_RBAC_Create_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "rbac-test-toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           []string{},
		DefaultEnvironmentSlug: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestToolsets_RBAC_Create_DeniedWithReadOnlyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, access.NewGrant(access.ScopeMCPRead, authCtx.ProjectID.String()))

	_, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "rbac-test-toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           []string{},
		DefaultEnvironmentSlug: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestToolsets_RBAC_Create_AllowedWithProjectWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, access.NewGrant(access.ScopeMCPWrite, authCtx.ProjectID.String()))

	_, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Name:                   "rbac-test-toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           []string{},
		DefaultEnvironmentSlug: nil,
	})
	require.NoError(t, err)
}

func TestToolsets_RBAC_CloneToolset_DeniedWithProjectWriteButNoSourceRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "rbac-clone-source-read-test")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, access.NewGrant(access.ScopeMCPWrite, authCtx.ProjectID.String()))

	_, err := ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             toolset.Slug,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestToolsets_RBAC_WriteOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "rbac-write-denied-test")

	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Slug:                   toolset.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestToolsets_RBAC_WriteOps_DeniedWithReadOnlyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "rbac-write-readonly-test")

	ctx = authztest.WithExactGrants(t, ctx, access.NewGrant(access.ScopeMCPRead, toolset.ID))

	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Slug:                   toolset.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestToolsets_RBAC_WriteOps_AllowedWithToolsetWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "rbac-write-allowed-test")

	ctx = authztest.WithExactGrants(t, ctx, access.NewGrant(access.ScopeMCPWrite, toolset.ID))

	name := "updated-name"
	_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		ProjectSlugInput:       nil,
		Slug:                   toolset.Slug,
		Name:                   &name,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpEnabled:             nil,
		McpIsPublic:            nil,
		CustomDomainID:         nil,
	})
	require.NoError(t, err)
}

func TestToolsets_RBAC_UpdateOAuthProxyServer_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "rbac-update-oauth-proxy-denied-test")

	ctx = authztest.WithExactGrants(t, ctx)

	audience := "https://api.example.com"
	_, err := ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             toolset.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{
			Audience: &audience,
		},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestToolsets_RBAC_UpdateOAuthProxyServer_DeniedWithReadOnlyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	toolset := createMinimalPrivateToolset(t, ctx, ti, "rbac-update-oauth-proxy-readonly-test")

	ctx = authztest.WithExactGrants(t, ctx, access.NewGrant(access.ScopeMCPRead, toolset.ID))

	audience := "https://api.example.com"
	_, err := ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             toolset.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{
			Audience: &audience,
		},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestToolsets_RBAC_UpdateOAuthProxyServer_EmptyForm_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withProAccount(t, ctx)
	toolset := createPublicToolsetWithCustomOAuthProxy(
		t, ctx, ti,
		"RBAC Empty Form OAuth Proxy Toolset",
		"rbac-empty-form-env",
		"rbac-empty-form-proxy",
	)

	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.UpdateOAuthProxyServer(ctx, &gen.UpdateOAuthProxyServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             toolset.Slug,
		OauthProxyServer: &types.OAuthProxyServerUpdateForm{},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
