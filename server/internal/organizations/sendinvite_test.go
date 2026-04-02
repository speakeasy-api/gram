package organizations_test

import (
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestService_SendInvite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339)
	createdAt := time.Now().UTC().Format(time.RFC3339)
	updatedAt := time.Now().UTC().Format(time.RFC3339)

	ti.orgs.On("SendInvitation", mock.Anything, thirdpartyworkos.SendInvitationOpts{
		Email:          "test@example.com",
		OrganizationID: "org_workos_test",
		InviterUserID:  authCtx.UserID,
		ExpiresInDays:  7,
	}).Return(&thirdpartyworkos.Invitation{
		ID:             "test-invitation-id",
		Email:          "test@example.com",
		State:          thirdpartyworkos.InvitationStatePending,
		OrganizationID: authCtx.ActiveOrganizationID,
		InviterUserID:  authCtx.UserID,
		ExpiresAt:      expiresAt,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil).Once()

	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{
		Email: "test@example.com",
	})
	require.NoError(t, err)
	require.NotNil(t, invite)
	require.Equal(t, "test-invitation-id", invite.ID)
	require.Equal(t, "test@example.com", invite.Email)
	require.Equal(t, "pending", invite.State)
	require.Equal(t, authCtx.ActiveOrganizationID, invite.OrganizationID)
	require.NotNil(t, invite.InviterUserID)
	require.Equal(t, authCtx.UserID, *invite.InviterUserID)
	require.NotNil(t, invite.ExpiresAt)
	require.Equal(t, expiresAt, *invite.ExpiresAt)
	require.Equal(t, createdAt, invite.CreatedAt)
	require.Equal(t, updatedAt, invite.UpdatedAt)
	require.Nil(t, invite.RoleSlug)
}

func TestService_SendInvite_WithRoleSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339)
	createdAt := time.Now().UTC().Format(time.RFC3339)
	updatedAt := time.Now().UTC().Format(time.RFC3339)

	roleSlug := "test-role"

	ti.orgs.On("SendInvitation", mock.Anything, thirdpartyworkos.SendInvitationOpts{
		Email:          "test@example.com",
		OrganizationID: "org_workos_test",
		InviterUserID:  authCtx.UserID,
		RoleSlug:       roleSlug,
		ExpiresInDays:  7,
	}).Return(&thirdpartyworkos.Invitation{
		ID:             "test-invitation-id",
		Email:          "test@example.com",
		State:          thirdpartyworkos.InvitationStatePending,
		OrganizationID: authCtx.ActiveOrganizationID,
		InviterUserID:  authCtx.UserID,
		ExpiresAt:      expiresAt,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil).Once()

	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{
		Email:    "test@example.com",
		RoleSlug: &roleSlug,
	})
	require.NoError(t, err)
	require.NotNil(t, invite)
	require.Equal(t, "test-invitation-id", invite.ID)
	require.NotNil(t, invite.RoleSlug)
	require.Equal(t, "test-role", *invite.RoleSlug)
}
