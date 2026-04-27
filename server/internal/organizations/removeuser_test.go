package organizations_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// WorkOS organization membership id stored on organization_user_relationships (not Gram user_id).
const testWorkosMembershipID = "org_membership_test_1"

// testOtherUserID is a different user from the authenticated admin, used as the removal target.
const testOtherUserID = "other-user-id"

func TestService_RemoveUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	_, err := userrepo.New(ti.conn).UpsertUser(ctx, userrepo.UpsertUserParams{
		ID: testOtherUserID, Email: "other@example.com", DisplayName: "Other User",
	})
	require.NoError(t, err)

	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         testOtherUserID,
	})
	require.NoError(t, err)
	err = orgrepo.New(ti.conn).AttachWorkOSUserToOrg(ctx, orgrepo.AttachWorkOSUserToOrgParams{
		OrganizationID:     authCtx.ActiveOrganizationID,
		UserID:             testOtherUserID,
		WorkosMembershipID: pgtype.Text{String: testWorkosMembershipID, Valid: true},
	})
	require.NoError(t, err)

	expectWorkOSOrgAdminRole(t, ti.orgs)

	ti.orgs.On("DeleteOrganizationMembership", mock.Anything, testWorkosMembershipID).Return(nil).Once()

	err = ti.service.RemoveUser(ctx, &gen.RemoveUserPayload{
		UserID: testOtherUserID,
	})
	require.NoError(t, err)

	rows, err := orgrepo.New(ti.conn).ListOrganizationUsers(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Empty(t, rows, "expected soft-deleted user to no longer appear in organization list")

	ti.orgs.AssertExpectations(t)
}

func TestService_RollsBackOnWorkOSError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	_, err := userrepo.New(ti.conn).UpsertUser(ctx, userrepo.UpsertUserParams{
		ID: testOtherUserID, Email: "other@example.com", DisplayName: "Other User",
	})
	require.NoError(t, err)

	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         testOtherUserID,
	})
	require.NoError(t, err)
	err = orgrepo.New(ti.conn).AttachWorkOSUserToOrg(ctx, orgrepo.AttachWorkOSUserToOrgParams{
		OrganizationID:     authCtx.ActiveOrganizationID,
		UserID:             testOtherUserID,
		WorkosMembershipID: pgtype.Text{String: testWorkosMembershipID, Valid: true},
	})
	require.NoError(t, err)

	expectWorkOSOrgAdminRole(t, ti.orgs)

	workosErr := errors.New("workos error")
	ti.orgs.On("DeleteOrganizationMembership", mock.Anything, testWorkosMembershipID).Return(workosErr).Once()

	err = ti.service.RemoveUser(ctx, &gen.RemoveUserPayload{
		UserID: testOtherUserID,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, workosErr)

	rows, err := orgrepo.New(ti.conn).ListOrganizationUsers(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, rows, 1, "transaction rollback should leave the organization_user_relationships row active")
	require.Equal(t, testOtherUserID, rows[0].UserID)

	ti.orgs.AssertExpectations(t)
}

func TestService_RemoveUser_NotAMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	expectWorkOSOrgAdminRole(t, ti.orgs)

	err := ti.service.RemoveUser(ctx, &gen.RemoveUserPayload{
		UserID: "non-member-user-id",
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
	require.Equal(t, "user is not a member of this organization", oopsErr.Error())

	ti.orgs.AssertExpectations(t)
}

func TestService_RemoveUser_CannotRemoveSelf(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	expectWorkOSOrgAdminRole(t, ti.orgs)

	err := ti.service.RemoveUser(ctx, &gen.RemoveUserPayload{
		UserID: authCtx.UserID,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_RemoveUser_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	_, err := userrepo.New(ti.conn).UpsertUser(ctx, userrepo.UpsertUserParams{
		ID: testOtherUserID, Email: "other@example.com", DisplayName: "Other User",
	})
	require.NoError(t, err)
	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         testOtherUserID,
	})
	require.NoError(t, err)

	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	ti.orgs.On("DeleteOrganizationMembership", mock.Anything, mock.Anything).Return(nil).Maybe()

	err = ti.service.RemoveUser(ctx, &gen.RemoveUserPayload{UserID: testOtherUserID})
	require.NoError(t, err)
}

func TestService_RemoveUser_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx)

	err := ti.service.RemoveUser(ctx, &gen.RemoveUserPayload{UserID: "any-user-id"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_RemoveUser_ForbiddenWithGrantForDifferentOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, "org_other"))

	err := ti.service.RemoveUser(ctx, &gen.RemoveUserPayload{UserID: "any-user-id"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_RemoveUser_ForbiddenWhenNotOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	expectWorkOSOrgNonAdminRole(t, ti.orgs)

	err := ti.service.RemoveUser(ctx, &gen.RemoveUserPayload{UserID: "any-user-id"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
