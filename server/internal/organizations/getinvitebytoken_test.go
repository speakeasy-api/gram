package organizations_test

import (
	"testing"

	mockidp "github.com/speakeasy-api/gram/mock-speakeasy-idp"
	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestService_GetInviteByToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.orgs.On("FindInvitationByToken", mock.Anything, "test-token").Return(&thirdpartyworkos.Invitation{
		ID:                  "test-invitation-id",
		Email:               "test@example.com",
		State:               thirdpartyworkos.InvitationStatePending,
		OrganizationID:      mockidp.MockOrgID,
		InviterUserID:       authCtx.UserID,
		AcceptInvitationURL: "https://auth.workos.com/invite/accept?token=test-token",
	}, nil).Once()

	res, err := ti.service.GetInviteByToken(ctx, &gen.GetInviteByTokenPayload{
		Token: "test-token",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "test@example.com", res.Email)
	require.Equal(t, "pending", res.State)
	require.Equal(t, mockidp.MockOrgName, res.OrganizationName)
	require.Equal(t, "https://auth.workos.com/invite/accept?token=test-token", res.AcceptInvitationURL)
}

func TestService_GetInviteByToken_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	_, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ti.orgs.On("FindInvitationByToken", mock.Anything, "test-token").Return(nil, &thirdpartyworkos.APIError{StatusCode: 404})

	res, err := ti.service.GetInviteByToken(ctx, &gen.GetInviteByTokenPayload{
		Token: "test-token",
	})
	require.Error(t, err)
	require.Nil(t, res)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)

	ti.orgs.AssertExpectations(t)
}
