package organizations_test

import (
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testWorkosOrgID = "org_workos_test"

func TestService_RevokeInvite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	expectWorkOSOrgAdminRole(t, ti.orgs)

	ti.orgs.On("GetInvitation", mock.Anything, "test-invitation-id").Return(&thirdpartyworkos.Invitation{
		ID:             "test-invitation-id",
		Email:          "test@example.com",
		State:          thirdpartyworkos.InvitationStatePending,
		OrganizationID: testWorkosOrgID,
		InviterUserID:  authCtx.UserID,
		ExpiresAt:      time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339),
	}, nil).Once()

	ti.orgs.On("RevokeInvitation", mock.Anything, "test-invitation-id").Return(&thirdpartyworkos.Invitation{
		ID:             "test-invitation-id",
		Email:          "test@example.com",
		State:          thirdpartyworkos.InvitationStateRevoked,
		OrganizationID: testWorkosOrgID,
		InviterUserID:  authCtx.UserID,
		ExpiresAt:      time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339),
	}, nil).Once()

	err := ti.service.RevokeInvite(ctx, &gen.RevokeInvitePayload{
		InvitationID: "test-invitation-id",
	})
	require.NoError(t, err)
}

func TestService_RevokeInvite_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	expectWorkOSOrgAdminRole(t, ti.orgs)

	ti.orgs.On("GetInvitation", mock.Anything, "test-invitation-id").Return(nil, &thirdpartyworkos.APIError{StatusCode: 404}).Once()

	err := ti.service.RevokeInvite(ctx, &gen.RevokeInvitePayload{
		InvitationID: "test-invitation-id",
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_RevokeInvite_WrongOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	expectWorkOSOrgAdminRole(t, ti.orgs)

	ti.orgs.On("GetInvitation", mock.Anything, "other-org-invitation-id").Return(&thirdpartyworkos.Invitation{
		ID:             "other-org-invitation-id",
		Email:          "victim@example.com",
		State:          thirdpartyworkos.InvitationStatePending,
		OrganizationID: "org_workos_someone_else",
		InviterUserID:  "user_01OTHER",
		ExpiresAt:      time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339),
	}, nil).Once()

	err := ti.service.RevokeInvite(ctx, &gen.RevokeInvitePayload{
		InvitationID: "other-org-invitation-id",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_RevokeInvite_ForbiddenWhenNotOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	expectWorkOSOrgNonAdminRole(t, ti.orgs)

	err := ti.service.RevokeInvite(ctx, &gen.RevokeInvitePayload{InvitationID: "any-invitation-id"})
	requireOrgManagementForbidden(t, err)
}
