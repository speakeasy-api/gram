package hooks

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks_server_names"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestHooks_RBAC_ReadOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestHooks_RBAC_ReadOps_AllowedWithBuildReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildRead, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestHooks_RBAC_ReadOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildWrite, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func TestHooks_RBAC_ReadOps_DeniedWithWrongResourceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildRead, Resource: uuid.NewString()})

	_, err := ti.service.List(ctx, &gen.ListPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestHooks_RBAC_WriteOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.Upsert(ctx, &gen.UpsertPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		RawServerName:    "test-server",
		DisplayName:      "Test Server",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestHooks_RBAC_WriteOps_DeniedWithReadOnlyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildRead, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.Upsert(ctx, &gen.UpsertPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		RawServerName:    "test-server",
		DisplayName:      "Test Server",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestHooks_RBAC_WriteOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildWrite, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.Upsert(ctx, &gen.UpsertPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		RawServerName:    "test-server",
		DisplayName:      "Test Server",
	})
	require.NoError(t, err)
}
