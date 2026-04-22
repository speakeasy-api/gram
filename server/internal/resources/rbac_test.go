package resources_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/resources"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestResources_RBAC_List_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestResourcesService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListResources(ctx, &gen.ListResourcesPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Cursor:           nil,
		Limit:            nil,
		DeploymentID:     nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestResources_RBAC_List_DeniedWithUnrelatedGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestResourcesService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.Grant{Scope: access.ScopeBuildWrite, Selector: access.ForResource("other-project-id")})

	_, err := ti.service.ListResources(ctx, &gen.ListResourcesPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Cursor:           nil,
		Limit:            nil,
		DeploymentID:     nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestResources_RBAC_List_AllowedWithBuildReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestResourcesService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.Grant{Scope: access.ScopeBuildRead, Selector: access.ForResource(authCtx.ProjectID.String())})

	result, err := ti.service.ListResources(ctx, &gen.ListResourcesPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Cursor:           nil,
		Limit:            nil,
		DeploymentID:     nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestResources_RBAC_List_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestResourcesService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.Grant{Scope: access.ScopeBuildWrite, Selector: access.ForResource(authCtx.ProjectID.String())})

	result, err := ti.service.ListResources(ctx, &gen.ListResourcesPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Cursor:           nil,
		Limit:            nil,
		DeploymentID:     nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}
