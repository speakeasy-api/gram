package access_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_CreateRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.AddMember("org_workos_test", "membership_1", "user_1", "member")
	ti.roles.AddMember("org_workos_test", "membership_2", "user_2", "member")

	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Custom Builder",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(access.ScopeBuildRead), Resources: []string{"project-1", "project-2"}},
			{Scope: string(access.ScopeMCPConnect), Resources: nil},
		},
		MemberIds: []string{"user_1", "user_2"},
	})
	require.NoError(t, err)
	require.Equal(t, "Custom Builder", role.Name)
	require.Equal(t, "Can build selected resources", role.Description)
	require.False(t, role.IsSystem)
	require.Equal(t, 2, role.MemberCount)
	require.Equal(t, time.Time{}.UTC().Format(time.RFC3339), role.CreatedAt)
	require.Equal(t, time.Time{}.UTC().Format(time.RFC3339), role.UpdatedAt)
	require.Len(t, role.Grants, 2)

	roles, err := ti.roles.ListRoles(ctx, "org_workos_test")
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, "custom-builder", roles[0].Slug)

	grants, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{PrincipalUrn: new(urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder").String())})
	require.NoError(t, err)
	require.Len(t, grants.Grants, 3)

	members, err := ti.roles.ListMembers(ctx, "org_workos_test")
	require.NoError(t, err)
	require.Len(t, members, 2)
	require.Equal(t, "custom-builder", members[0].RoleSlug)
	require.Equal(t, "custom-builder", members[1].RoleSlug)
}

func TestService_CreateRole_WorkOSCreateFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.SetCreateRoleError(errors.New("workos unavailable"))

	_, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Custom Builder",
		Description: "Can build selected resources",
		Grants: []*gen.RoleGrant{
			{Scope: string(access.ScopeBuildRead), Resources: []string{"project-1"}},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "create role in workos")
}

func TestService_CreateRole_GrantSyncFailureDoesNotAssignMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	ti.roles.AddMember("org_workos_test", "membership_1", "user_1", "member")
	ti.roles.AddMember("org_workos_test", "membership_2", "user_2", "member")

	inspectConn, err := pgxpool.New(ctx, ti.conn.Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(inspectConn.Close)

	ti.roles.SetAfterCreateRole(func() {
		ti.conn.Close()
	})

	_, err = ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Broken Builder",
		Description: "Will fail grant sync",
		Grants: []*gen.RoleGrant{
			{Scope: string(access.ScopeBuildRead), Resources: []string{"project-1"}},
		},
		MemberIds: []string{"user_1", "user_2"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "sync grants for created role")

	roles, err := ti.roles.ListRoles(ctx, "org_workos_test")
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, "broken-builder", roles[0].Slug)

	members, err := ti.roles.ListMembers(ctx, "org_workos_test")
	require.NoError(t, err)
	require.Len(t, members, 2)
	require.Equal(t, "member", members[0].RoleSlug)
	require.Equal(t, "member", members[1].RoleSlug)

	grants, err := accessrepo.New(inspectConn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, "broken-builder").String(),
	})
	require.NoError(t, err)
	require.Empty(t, grants)
}

//go:fix inline
func stringPtr(v string) *string {
	return new(v)
}
