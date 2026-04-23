package keys_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestKeysService_CreateKey_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "rbac-denied-create", Scopes: []string{"consumer"}})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestKeysService_CreateKey_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Resource: testAuthContext(t, ctx).ActiveOrganizationID})

	key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "rbac-allow-create", Scopes: []string{"consumer"}})
	require.NoError(t, err)
	require.Equal(t, "rbac-allow-create", key.Name)
}

func TestKeysService_ListKeys_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestKeysService_ListKeys_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Resource: testAuthContext(t, ctx).ActiveOrganizationID})
	_, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "rbac-allow-list", Scopes: []string{"consumer"}})
	require.NoError(t, err)

	result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{})
	require.NoError(t, err)
	require.Len(t, result.Keys, 1)
}

func TestKeysService_RevokeKey_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Resource: testAuthContext(t, ctx).ActiveOrganizationID})
	key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "rbac-denied-revoke", Scopes: []string{"consumer"}})
	require.NoError(t, err)

	ctx = authztest.WithExactGrants(t, ctx)
	err = ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{ID: key.ID})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestKeysService_RevokeKey_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Resource: testAuthContext(t, ctx).ActiveOrganizationID})
	key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "rbac-allow-revoke", Scopes: []string{"consumer"}})
	require.NoError(t, err)

	err = ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{ID: key.ID})
	require.NoError(t, err)
}

func testAuthContext(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	return authCtx
}
