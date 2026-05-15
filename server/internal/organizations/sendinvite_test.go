package organizations_test

import (
	"fmt"
	"testing"

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

	ti.orgs.On("CreatePasswordlessSession", mock.Anything, mock.Anything).Return(&thirdpartyworkos.PasswordlessSession{
		ID:   "pwl_123",
		Link: "https://stub.workos.com/passwordless/123",
	}, nil).Once()

	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{
		Email: "test@example.com",
	})
	require.NoError(t, err)
	require.NotNil(t, invite)
	require.Equal(t, "test@example.com", invite.Email)
	require.Equal(t, "pending", invite.State)
	require.NotNil(t, invite.InviterUserID)
	require.Equal(t, authCtx.UserID, *invite.InviterUserID)
	require.NotEmpty(t, invite.ID)
	require.NotEmpty(t, invite.CreatedAt)
}

func TestService_SendInvite_WithRoleID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	roleID := "test-role"

	ti.orgs.On("ListRoles", mock.Anything, mock.Anything).Return([]thirdpartyworkos.Role{
		{ID: "test-role", Slug: "member", Name: "Member"},
	}, nil).Once()

	ti.orgs.On("CreatePasswordlessSession", mock.Anything, mock.Anything).Return(&thirdpartyworkos.PasswordlessSession{
		ID:   "pwl_456",
		Link: "https://stub.workos.com/passwordless/456",
	}, nil).Once()

	invite, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{
		Email:  "test@example.com",
		RoleID: &roleID,
	})
	require.NoError(t, err)
	require.NotNil(t, invite)
	require.Equal(t, "test@example.com", invite.Email)
}

func TestService_SendInvite_AllowsOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	ti.orgs.On("CreatePasswordlessSession", mock.Anything, mock.Anything).Return(&thirdpartyworkos.PasswordlessSession{
		ID:   "pwl_789",
		Link: "https://stub.workos.com/passwordless/789",
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

func TestService_SendInvite_DuplicatePendingEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	ti.orgs.On("CreatePasswordlessSession", mock.Anything, mock.Anything).Return(&thirdpartyworkos.PasswordlessSession{
		ID: "pwl_dup", Link: "https://stub.workos.com/passwordless/dup",
	}, nil).Once()

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "dup@example.com"})
	require.NoError(t, err)

	// Second invite to same email in same org should fail (partial unique index).
	_, err = ti.service.SendInvite(ctx, &gen.SendInvitePayload{Email: "dup@example.com"})
	require.Error(t, err, "duplicate pending invite for same email should fail")
}

func TestService_SendInvite_UnknownRoleID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	roleID := "nonexistent-role"

	ti.orgs.On("ListRoles", mock.Anything, mock.Anything).Return([]thirdpartyworkos.Role{
		{ID: "some-other-role", Slug: "member", Name: "Member"},
	}, nil).Once()

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{
		Email:  "test@example.com",
		RoleID: &roleID,
	})
	require.Error(t, err, "should fail when role ID not found in ListRoles result")
}

func TestService_SendInvite_FailsWhenPasswordlessSessionFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)

	ti.orgs.On("CreatePasswordlessSession", mock.Anything, mock.Anything).Return(
		(*thirdpartyworkos.PasswordlessSession)(nil), fmt.Errorf("workos unavailable"),
	).Once()

	_, err := ti.service.SendInvite(ctx, &gen.SendInvitePayload{
		Email: "nobody@example.com",
	})
	require.Error(t, err, "should fail when passwordless session creation fails")
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
