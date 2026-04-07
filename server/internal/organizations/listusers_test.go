package organizations_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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
			require.NotEmpty(t, u.CreatedAt)
			require.NotEmpty(t, u.UpdatedAt)
			break
		}
	}
	require.True(t, found, "expected upserted user in list")
}

func TestService_ListUsers_NoActiveOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotEmpty(t, authCtx.ActiveOrganizationID, "setup should have active org")

	stripped := *authCtx
	stripped.ActiveOrganizationID = ""
	ctx = contextvalues.SetAuthContext(ctx, &stripped)

	res, err := ti.service.ListUsers(ctx, &gen.ListUsersPayload{})
	require.Error(t, err)
	require.Nil(t, res)
}

func TestService_ListUsers_ForbiddenWhenNotOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	expectWorkOSOrgNonAdminRole(t, ti.orgs)

	res, err := ti.service.ListUsers(ctx, &gen.ListUsersPayload{})
	requireOrgManagementForbidden(t, err)
	require.Nil(t, res)
}
