package keys_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestKeysService_CreateKey_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "rbac-denied-create", Scopes: []string{"consumer"}})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestKeysService_CreateKey_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Resource: testAuthContext(t, ctx).ActiveOrganizationID})

	key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "rbac-allow-create", Scopes: []string{"consumer"}})
	require.NoError(t, err)
	require.Equal(t, "rbac-allow-create", key.Name)
}

func TestKeysService_ListKeys_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestKeysService_ListKeys_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Resource: testAuthContext(t, ctx).ActiveOrganizationID})
	_, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "rbac-allow-list", Scopes: []string{"consumer"}})
	require.NoError(t, err)

	result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{})
	require.NoError(t, err)
	require.Len(t, result.Keys, 1)
}

func TestKeysService_RevokeKey_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Resource: testAuthContext(t, ctx).ActiveOrganizationID})
	key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "rbac-denied-revoke", Scopes: []string{"consumer"}})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn)
	err = ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{ID: key.ID})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestKeysService_RevokeKey_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Resource: testAuthContext(t, ctx).ActiveOrganizationID})
	key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "rbac-allow-revoke", Scopes: []string{"consumer"}})
	require.NoError(t, err)

	err = ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{ID: key.ID})
	require.NoError(t, err)
}

func withExactAccessGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, grants ...access.Grant) context.Context {
	t.Helper()

	authCtx := testAuthContext(t, ctx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "keys-rbac-grants-"+uuid.NewString())
	for _, grant := range grants {
		_, err := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   principal,
			Scope:          string(grant.Scope),
			Resource:       grant.Resource,
		})
		require.NoError(t, err)
	}

	loadedGrants, err := access.LoadGrants(ctx, conn, authCtx.ActiveOrganizationID, []urn.Principal{principal})
	require.NoError(t, err)

	return access.GrantsToContext(ctx, loadedGrants)
}

func testAuthContext(t *testing.T, ctx context.Context) *contextvalues.AuthContext {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	return authCtx
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
