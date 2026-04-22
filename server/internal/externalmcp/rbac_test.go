package externalmcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestExternalMCP_RBAC_ReadOps_DeniedWithNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListCatalog(ctx, &gen.ListCatalogPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		RegistryID:       nil,
		Search:           nil,
		Cursor:           nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestExternalMCP_RBAC_ReadOps_AllowedWithBuildReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.Grant{Scope: access.ScopeBuildRead, Selector: access.ForResource(authCtx.ProjectID.String())})

	_, err := ti.service.ListCatalog(ctx, &gen.ListCatalogPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		RegistryID:       nil,
		Search:           nil,
		Cursor:           nil,
	})
	require.NoError(t, err)
}

func TestExternalMCP_RBAC_ReadOps_AllowedWithBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.Grant{Scope: access.ScopeBuildWrite, Selector: access.ForResource(authCtx.ProjectID.String())})

	_, err := ti.service.ListCatalog(ctx, &gen.ListCatalogPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		RegistryID:       nil,
		Search:           nil,
		Cursor:           nil,
	})
	require.NoError(t, err)
}

func TestExternalMCP_RBAC_ReadOps_DeniedWithWrongResourceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)
	ctx = rbactest.WithExactAccessGrants(t, ctx, access.Grant{Scope: access.ScopeBuildRead, Selector: access.ForResource(uuid.NewString())})

	_, err := ti.service.ListCatalog(ctx, &gen.ListCatalogPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		RegistryID:       nil,
		Search:           nil,
		Cursor:           nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
