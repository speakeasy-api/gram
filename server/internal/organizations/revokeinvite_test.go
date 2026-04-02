package organizations_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestService_RevokeInvite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.orgs.On("RevokeInvitation", mock.Anything, "test-invitation-id").Return(&thirdpartyworkos.Invitation{
		ID:             "test-invitation-id",
		Email:          "test@example.com",
		State:          thirdpartyworkos.InvitationStateRevoked,
		OrganizationID: authCtx.ActiveOrganizationID,
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

	notFound := fmt.Errorf("not found")
	ti.orgs.On("RevokeInvitation", mock.Anything, "test-invitation-id").Return(nil, notFound).Once()

	err := ti.service.RevokeInvite(ctx, &gen.RevokeInvitePayload{
		InvitationID: "test-invitation-id",
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, notFound), "expected not-found error, got %v", err)
}
