package organizations_test

import (
	"testing"
	"time"

	mockidp "github.com/speakeasy-api/gram/server/internal/testenv/testidp"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

	expectWorkOSOrgAdminRole(t, ti.orgs)

	ti.orgs.On("SendInvitation", mock.Anything, thirdpartyworkos.SendInvitationOpts{
		Email:          "test@example.com",
		OrganizationID: mockidp.MockOrgID,
		InviterUserID:  testAuthUserWorkOSID,
		ExpiresInDays:  7,
	}).Return(&thirdpartyworkos.Invitation{
		ID:             "test-invitation-id",
		Email:          "test@example.com",
		State:          thirdpartyworkos.InvitationStatePending,
		OrganizationID: "org_01WORKOS",
		InviterUserID:  "user_01WORKOS",
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
	require.NotNil(t, invite.InviterUserID)
	require.Equal(t, authCtx.UserID, *invite.InviterUserID)
	require.NotNil(t, invite.ExpiresAt)
	require.Equal(t, expiresAt, *invite.ExpiresAt)
	require.Equal(t, createdAt, invite.CreatedAt)
	require.Equal(t, updatedAt, invite.UpdatedAt)
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

	expectWorkOSOrgAdminRole(t, ti.orgs)

	ti.orgs.On("SendInvitation", mock.Anything, thirdpartyworkos.SendInvitationOpts{
		Email:          "test@example.com",
		OrganizationID: mockidp.MockOrgID,
		InviterUserID:  testAuthUserWorkOSID,
		RoleSlug:       roleSlug,
		ExpiresInDays:  7,
	}).Return(&thirdpartyworkos.Invitation{
		ID:             "test-invitation-id",
		Email:          "test@example.com",
		State:          thirdpartyworkos.InvitationStatePending,
		OrganizationID: "org_01WORKOS",
		InviterUserID:  "user_01WORKOS",
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
}

func TestService_SendInvite_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339)
	createdAt := time.Now().UTC().Format(time.RFC3339)
	updatedAt := time.Now().UTC().Format(time.RFC3339)

	ti.orgs.On("SendInvitation", mock.Anything, mock.Anything).Return(&thirdpartyworkos.Invitation{
		ID:             "test-invitation-id",
		Email:          "test@example.com",
		State:          thirdpartyworkos.InvitationStatePending,
		OrganizationID: "org_01WORKOS",
		InviterUserID:  "user_01WORKOS",
		ExpiresAt:      expiresAt,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil).Once()

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "x@example.com"})
	require.NoError(t, err)
}

func TestService_SendInvite_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "x@example.com"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_SendInvite_ForbiddenWithGrantForDifferentOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, "org_other")})

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "x@example.com"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_SendInvite_ForbiddenWhenNotOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	expectWorkOSOrgNonAdminRole(t, ti.orgs)

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "x@example.com"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
