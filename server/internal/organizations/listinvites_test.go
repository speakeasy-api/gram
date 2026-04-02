package organizations_test

import (
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestService_ListInvites(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	now := time.Now().UTC()
	expiresAt := now.Add(7 * 24 * time.Hour).Format(time.RFC3339)
	createdAt := now.Format(time.RFC3339)
	updatedAt := now.Format(time.RFC3339)

	ti.orgs.On("ListInvitations", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Invitation{
		{
			ID:             "test-invitation-id",
			Email:          "test@example.com",
			State:          thirdpartyworkos.InvitationStatePending,
			OrganizationID: authCtx.ActiveOrganizationID,
			InviterUserID:  authCtx.UserID,
			ExpiresAt:      expiresAt,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		},
		{
			ID:             "test-invitation-id-2",
			Email:          "test2@example.com",
			State:          thirdpartyworkos.InvitationStateAccepted,
			OrganizationID: authCtx.ActiveOrganizationID,
			InviterUserID:  authCtx.UserID,
			ExpiresAt:      expiresAt,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		},
	}, nil).Once()

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Invitations, 1)

	inv0 := res.Invitations[0]
	require.Equal(t, "test-invitation-id", inv0.ID)
	require.Equal(t, "test@example.com", inv0.Email)
	require.Equal(t, "pending", inv0.State)
	require.Equal(t, authCtx.ActiveOrganizationID, inv0.OrganizationID)
	require.NotNil(t, inv0.InviterUserID)
	require.Equal(t, authCtx.UserID, *inv0.InviterUserID)
	require.NotNil(t, inv0.ExpiresAt)
	require.Equal(t, expiresAt, *inv0.ExpiresAt)
	require.Equal(t, createdAt, inv0.CreatedAt)
	require.Equal(t, updatedAt, inv0.UpdatedAt)
}
