package organizations_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/stretchr/testify/require"
)

func TestService_ListUsers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	expectWorkOSOrgAdminRole(t, ti.orgs)

	_, err := orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
	})
	require.NoError(t, err)

	res, err := ti.service.ListUsers(ctx, &gen.ListUsersPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.GreaterOrEqual(t, len(res.Users), 1)

	var found bool
	for _, u := range res.Users {
		if u.UserID == authCtx.UserID && u.OrganizationID == authCtx.ActiveOrganizationID {
			found = true
			require.NotEmpty(t, u.ID)
			require.NotEmpty(t, u.Name)
			require.NotEmpty(t, u.Email)
			require.NotEmpty(t, u.CreatedAt)
			require.NotEmpty(t, u.UpdatedAt)
			break
		}
	}
	require.True(t, found, "expected upserted user in list")
}

func TestService_ListUsers_AllowsOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	_, err := orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
	})
	require.NoError(t, err)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.NewGrant(access.ScopeOrgRead, authCtx.ActiveOrganizationID))

	res, err := ti.service.ListUsers(ctx, &gen.ListUsersPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestService_ListUsers_AllowsOrgAdminGrantViaScopeHierarchy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	_, err := orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
	})
	require.NoError(t, err)

	ctx = rbactest.WithExactAccessGrants(t, ctx, access.NewGrant(access.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	res, err := ti.service.ListUsers(ctx, &gen.ListUsersPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestService_ListUsers_ForbiddenWithoutOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx)

	res, err := ti.service.ListUsers(ctx, &gen.ListUsersPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Nil(t, res)
}

func TestService_ListUsers_ForbiddenWithGrantForDifferentOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = rbactest.WithExactAccessGrants(t, ctx, access.NewGrant(access.ScopeOrgAdmin, "org_other"))

	res, err := ti.service.ListUsers(ctx, &gen.ListUsersPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Nil(t, res)
}

func TestService_ListUsers_ForbiddenWhenNotOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	expectWorkOSOrgNonAdminRole(t, ti.orgs)

	res, err := ti.service.ListUsers(ctx, &gen.ListUsersPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Nil(t, res)
}
