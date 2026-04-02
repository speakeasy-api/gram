package organizations_test

import (
	"errors"
	"fmt"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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
		OrganizationID:      authCtx.ActiveOrganizationID,
		InviterUserID:       authCtx.UserID,
		AcceptInvitationURL: "https://auth.workos.com/invite/accept",
	}, nil).Once()

	res, err := ti.service.GetInviteByToken(ctx, &gen.GetInviteByTokenPayload{
		Token: "test-token",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "test@example.com", res.Email)
	require.Equal(t, "pending", res.State)
	require.Equal(t, "https://auth.workos.com/invite/accept", res.AcceptInvitationURL)
}

func TestService_GetInviteByToken_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	_, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	notFound := fmt.Errorf("not found")

	ti.orgs.On("FindInvitationByToken", mock.Anything, "test-token").Return(nil, notFound)

	res, err := ti.service.GetInviteByToken(ctx, &gen.GetInviteByTokenPayload{
		Token: "test-token",
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, notFound), "expected not-found error, got %v", err)
	require.Nil(t, res)
}
