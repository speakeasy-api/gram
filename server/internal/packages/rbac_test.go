package packages_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/packages"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestPackages_RBAC_ReadOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPackagesService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListPackages(ctx, &gen.ListPackagesPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestPackages_RBAC_ReadOps_AllowedWithBuildReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPackagesService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.NewGrant(access.ScopeBuildRead, authCtx.ProjectID.String()))

	_, err := ti.service.ListPackages(ctx, &gen.ListPackagesPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestPackages_RBAC_ReadOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPackagesService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.NewGrant(access.ScopeBuildWrite, authCtx.ProjectID.String()))

	_, err := ti.service.ListPackages(ctx, &gen.ListPackagesPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestPackages_RBAC_ReadOps_DeniedWithWrongResourceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPackagesService(t)
	ctx = rbactest.WithExactAccessGrants(t, ctx, access.NewGrant(access.ScopeBuildRead, uuid.NewString()))

	_, err := ti.service.ListPackages(ctx, &gen.ListPackagesPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestPackages_RBAC_WriteOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPackagesService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.CreatePackage(ctx, &gen.CreatePackagePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             "rbac-test-pkg",
		Title:            "RBAC Test Package",
		Summary:          "test",
		Description:      nil,
		URL:              nil,
		Keywords:         []string{},
		ImageAssetID:     nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestPackages_RBAC_WriteOps_DeniedWithReadOnlyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPackagesService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.NewGrant(access.ScopeBuildRead, authCtx.ProjectID.String()))

	_, err := ti.service.CreatePackage(ctx, &gen.CreatePackagePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             "rbac-test-pkg",
		Title:            "RBAC Test Package",
		Summary:          "test",
		Description:      nil,
		URL:              nil,
		Keywords:         []string{},
		ImageAssetID:     nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestPackages_RBAC_WriteOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPackagesService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.NewGrant(access.ScopeBuildWrite, authCtx.ProjectID.String()))

	_, err := ti.service.CreatePackage(ctx, &gen.CreatePackagePayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             "rbac-test-pkg",
		Title:            "RBAC Test Package",
		Summary:          "test",
		Description:      nil,
		URL:              nil,
		Keywords:         []string{},
		ImageAssetID:     nil,
	})
	require.NoError(t, err)
}
