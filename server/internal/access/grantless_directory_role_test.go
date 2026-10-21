package access

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Bulk directory setup creates roles with no permissions and maps directory
// sources to them. This walks that journey through the real service: the role
// is created with an empty grant list, reaches WorkOS with only its identity,
// is mapped from a directory group, and is held by the group's member at
// evaluation time without changing what the member can do.
func TestService_GrantlessRole_CreateMapAndEvaluate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID

	const roleName = "Platform Engineers"
	slug, err := slugify(roleName)
	require.NoError(t, err)

	// WorkOS receives the role's name, slug and description and nothing else.
	// The fixture's cleanup fails the test if this call never happens.
	ti.roles.On("CreateRole", mock.Anything, mockidp.MockOrgID, thirdpartyworkos.CreateRoleOpts{
		Name:        roleName,
		Slug:        slug,
		Description: "",
	}).Return(&thirdpartyworkos.Role{
		ID:        "role_platform_engineers",
		Name:      roleName,
		Slug:      slug,
		CreatedAt: mockRoleTimestamp,
		UpdatedAt: mockRoleTimestamp,
	}, nil).Once()

	seedConnectedUser(t, ctx, ti.conn, orgID, "local_eng_user", "eng@test.com", "Eng User", "user_eng", "membership_eng")
	directoryUserID, workosUserID := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "local_eng_user", "eng@test.com", `{}`)
	groupID, workosGroupID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, roleName)
	addMappingGroupMember(t, ctx, ti.conn, directoryUserID, workosUserID, groupID, workosGroupID)

	principalsBefore, err := authz.ResolveUserPrincipals(ctx, ti.conn, orgID, "local_eng_user")
	require.NoError(t, err)
	grantsBefore, err := authz.LoadGrants(ctx, ti.conn, orgID, principalsBefore)
	require.NoError(t, err)

	// Create: an empty grant list is accepted and stored as no grants.
	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:   roleName,
		Grants: []*gen.RoleGrant{},
	})
	require.NoError(t, err)
	require.Empty(t, role.Grants)
	rolePrincipal, err := urn.ParsePrincipal(role.PrincipalUrn)
	require.NoError(t, err)
	require.Empty(t, listPrincipalGrants(t, ctx, ti.conn, orgID, rolePrincipal))

	// Creating a role does not set up plugin distribution for it.
	plugins, err := pluginsrepo.New(ti.conn).ListPluginsForRoleDeletion(ctx, pluginsrepo.ListPluginsForRoleDeletionParams{
		OrganizationID: orgID,
		PrincipalUrn:   role.PrincipalUrn,
	})
	require.NoError(t, err)
	require.Empty(t, plugins)

	// Map: the directory group grants the new role.
	groupIDString := groupID.String()
	mappings, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &groupIDString,
		RoleUrns:         []string{role.PrincipalUrn},
	})
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	require.Equal(t, role.PrincipalUrn, mappings[0].RoleUrn)
	require.Equal(t, directoryRoleMappingSourceActive, mappings[0].SourceStatus)

	// Evaluate: the member now holds the role, and it adds no grants.
	principalsAfter, err := authz.ResolveUserPrincipals(ctx, ti.conn, orgID, "local_eng_user")
	require.NoError(t, err)
	require.Contains(t, principalsAfter, rolePrincipal)
	require.NotContains(t, principalsBefore, rolePrincipal)
	grantsAfter, err := authz.LoadGrants(ctx, ti.conn, orgID, principalsAfter)
	require.NoError(t, err)
	require.ElementsMatch(t, grantsBefore, grantsAfter)

	// The role and member reads report the mapped membership.
	stored, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: role.ID})
	require.NoError(t, err)
	require.Empty(t, stored.Grants)
	require.Equal(t, 1, stored.MemberCount)

	members, err := ti.service.ListMembers(ctx, &gen.ListMembersPayload{})
	require.NoError(t, err)
	var member *gen.AccessMember
	for _, m := range members.Members {
		if m.Email == "eng@test.com" {
			member = m
		}
	}
	require.NotNil(t, member)
	require.Contains(t, member.DirectoryRoleIds, role.ID)
	require.True(t, conv.PtrValOr(member.InDirectory, false))
	require.Len(t, member.DirectoryRoleSources, 1)
	require.Equal(t, role.ID, member.DirectoryRoleSources[0].RoleID)
	require.Equal(t, mappings[0].ID, member.DirectoryRoleSources[0].MappingID)
	require.Equal(t, groupIDString, conv.PtrValOr(member.DirectoryRoleSources[0].DirectoryGroupID, ""))

	// Still no plugin distribution once the role has a member.
	plugins, err = pluginsrepo.New(ti.conn).ListPluginsForRoleDeletion(ctx, pluginsrepo.ListPluginsForRoleDeletionParams{
		OrganizationID: orgID,
		PrincipalUrn:   role.PrincipalUrn,
	})
	require.NoError(t, err)
	require.Empty(t, plugins)
}
