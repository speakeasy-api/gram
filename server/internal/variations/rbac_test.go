package variations_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/variations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/rbactest"
)

func TestVariations_RBAC_ReadOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)
	ctx = rbactest.WithExactAccessGrants(t, ctx)

	_, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestVariations_RBAC_ReadOps_AllowedWithBuildReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, authz.Grant{Scope: authz.ScopeBuildRead, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestVariations_RBAC_ReadOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, authz.Grant{Scope: authz.ScopeBuildWrite, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestVariations_RBAC_ReadOps_DeniedWithWrongResourceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)
	ctx = rbactest.WithExactAccessGrants(t, ctx, authz.Grant{Scope: authz.ScopeBuildRead, Resource: uuid.NewString()})

	_, err := ti.service.ListGlobal(ctx, &gen.ListGlobalPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestVariations_RBAC_WriteOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)
	ctx = rbactest.WithExactAccessGrants(t, ctx)

	_, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:my-source:my-tool",
		SrcToolName:      "my-tool",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestVariations_RBAC_WriteOps_DeniedWithReadOnlyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, authz.Grant{Scope: authz.ScopeBuildRead, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:my-source:my-tool",
		SrcToolName:      "my-tool",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestVariations_RBAC_WriteOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, authz.Grant{Scope: authz.ScopeBuildWrite, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.UpsertGlobal(ctx, &gen.UpsertGlobalPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SrcToolUrn:       "tools:http:my-source:my-tool",
		SrcToolName:      "my-tool",
	})
	require.NoError(t, err)
}
