package organizations_test

import (
	"errors"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Matches SetOrgWorkosID in setup_test.go.
const testWorkosOrgID = "org_workos_test"

func TestService_RemoveUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	_, err := orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
	})
	require.NoError(t, err)

	ti.orgs.On("RemoveUser", mock.Anything, testWorkosOrgID, authCtx.UserID).Return(nil).Once()

	err = ti.service.RemoveUser(ctx, &gen.RemoveUserPayload{
		UserID: authCtx.UserID,
	})
	require.NoError(t, err)

	rows, err := orgrepo.New(ti.conn).ListOrganizationUsers(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, rows, "expected soft-deleted user to no longer appear in organization list")

	// Check that the user is no longer a member of the organization
	users, err := ti.service.ListUsers(ctx, &gen.ListUsersPayload{})
	require.NoError(t, err)
	require.Empty(t, users.Users, "expected user to be removed from organization")

	ti.orgs.AssertExpectations(t)
}

func TestService_RollsBackOnWorkOSError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	_, err := orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         authCtx.UserID,
	})
	require.NoError(t, err)

	workosErr := errors.New("workos error")
	ti.orgs.On("RemoveUser", mock.Anything, testWorkosOrgID, authCtx.UserID).Return(workosErr).Once()

	err = ti.service.RemoveUser(ctx, &gen.RemoveUserPayload{
		UserID: authCtx.UserID,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, workosErr)

	rows, err := orgrepo.New(ti.conn).ListOrganizationUsers(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, rows, 1, "transaction rollback should leave the organization_user_relationships row active")
	require.Equal(t, authCtx.UserID, rows[0].UserID)

	ti.orgs.AssertExpectations(t)
}

// Test that removing a user that is not a member of the organization returns a not found error.
func TestService_RemoveUser_NotAMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	err := ti.service.RemoveUser(ctx, &gen.RemoveUserPayload{
		UserID: "non-member-user-id",
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
	require.Equal(t, "user is not a member of this organization", oopsErr.Error())

	ti.orgs.AssertExpectations(t)
}
