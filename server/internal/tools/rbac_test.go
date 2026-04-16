package tools_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/tools"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestTools_RBAC_ReadOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsService(t, assetstest.NewTestBlobStore(t))
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     nil,
		Cursor:           nil,
		Limit:            nil,
		UrnPrefix:        nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestTools_RBAC_ReadOps_AllowedWithBuildReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsService(t, assetstest.NewTestBlobStore(t))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildRead, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     nil,
		Cursor:           nil,
		Limit:            nil,
		UrnPrefix:        nil,
	})
	require.NoError(t, err)
}

func TestTools_RBAC_ReadOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsService(t, assetstest.NewTestBlobStore(t))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildWrite, Resource: authCtx.ProjectID.String()})

	_, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     nil,
		Cursor:           nil,
		Limit:            nil,
		UrnPrefix:        nil,
	})
	require.NoError(t, err)
}

func TestTools_RBAC_ReadOps_DeniedWithWrongResourceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsService(t, assetstest.NewTestBlobStore(t))
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeMCPRead, Resource: uuid.NewString()})

	_, err := ti.service.ListTools(ctx, &gen.ListToolsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		DeploymentID:     nil,
		Cursor:           nil,
		Limit:            nil,
		UrnPrefix:        nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
