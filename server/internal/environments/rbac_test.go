package environments_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestEnvironments_RBAC_ReadOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestEnvironments_RBAC_ReadOps_AllowedWithBuildReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildRead, Resource: authCtx.ProjectID.String()})

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

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildWrite, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestEnvironments_RBAC_ReadOps_DeniedWithWrongResourceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildRead, Resource: uuid.NewString()})

	_, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestEnvironments_RBAC_WriteOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-test-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestEnvironments_RBAC_WriteOps_DeniedWithReadOnlyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildRead, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "rbac-test-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestEnvironments_RBAC_WriteOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildWrite, Resource: authCtx.ProjectID.String()})

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
