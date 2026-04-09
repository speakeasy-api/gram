package organizations_test

import (
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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

	const workosInviterUserID = "user_01WORKOS_INVITER"
	require.NoError(t, userrepo.New(ti.conn).SetUserWorkosID(ctx, userrepo.SetUserWorkosIDParams{
		ID:       authCtx.UserID,
		WorkosID: conv.ToPGText(workosInviterUserID),
	}))

	expectWorkOSOrgAdminRole(t, ti.orgs)

	ti.orgs.On("ListInvitations", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Invitation{
		{
			ID:             "test-invitation-id",
			Email:          "test@example.com",
			State:          thirdpartyworkos.InvitationStatePending,
			OrganizationID: "org_workos_test",
			InviterUserID:  workosInviterUserID,
			ExpiresAt:      expiresAt,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		},
		{
			ID:             "test-invitation-id-2",
			Email:          "test2@example.com",
			State:          thirdpartyworkos.InvitationStateAccepted,
			OrganizationID: "org_workos_test",
			InviterUserID:  workosInviterUserID,
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
	require.NotNil(t, inv0.InviterUserID)
	require.Equal(t, authCtx.UserID, *inv0.InviterUserID)
	require.NotNil(t, inv0.ExpiresAt)
	require.Equal(t, expiresAt, *inv0.ExpiresAt)
	require.Equal(t, createdAt, inv0.CreatedAt)
	require.Equal(t, updatedAt, inv0.UpdatedAt)
}

func TestService_ListInvites_AllowsOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgRead, Resource: authCtx.ActiveOrganizationID})

	ti.orgs.On("ListInvitations", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Invitation{}, nil).Once()

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestService_ListInvites_AllowsOrgAdminGrantViaScopeHierarchy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Resource: authCtx.ActiveOrganizationID})

	ti.orgs.On("ListInvitations", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Invitation{}, nil).Once()

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestService_ListInvites_ForbiddenWithoutOrgReadGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Nil(t, res)
}

func TestService_ListInvites_ForbiddenWithGrantForDifferentOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceRBAC(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Resource: "org_other"})

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Nil(t, res)
}

func TestService_ListInvites_ForbiddenWhenNotOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	expectWorkOSOrgNonAdminRole(t, ti.orgs)

	res, err := ti.service.ListInvites(ctx, &gen.ListInvitesPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Nil(t, res)
}
