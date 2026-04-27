package environments_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestEnvironments_RBAC_ReadOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_ReadOps_AllowedWithBuildReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))

	_, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestEnvironments_RBAC_ReadOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))

	_, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestEnvironments_RBAC_ReadOps_DeniedWithWrongResourceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectRead, uuid.NewString()))

	_, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_WriteOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-test-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_WriteOps_DeniedWithReadOnlyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))

	_, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-test-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestEnvironments_RBAC_WriteOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))

	_, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-test-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)
}
