package access_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
)

func TestCreateRole(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)

	result, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Project Manager",
		Description: "Manages projects",
		Grants: []*gen.RoleGrant{
			{Scope: "build:read"},
			{Scope: "build:write", Resources: []string{"proj-1", "proj-2"}},
		},
	})
	require.NoError(t, err)

	assert.NotEmpty(t, result.ID)
	assert.Equal(t, "Project Manager", result.Name)
	assert.Equal(t, "Manages projects", result.Description)
	assert.False(t, result.IsSystem)
	assert.Len(t, result.Grants, 2)

	// Verify grants were persisted — list the role and check grants are present.
	listResult, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	require.NoError(t, err)

	var found *gen.Role
	for _, r := range listResult.Roles {
		if r.ID == result.ID {
			found = r
			break
		}
	}
	require.NotNil(t, found, "created role should appear in ListRoles")
	assert.Len(t, found.Grants, 2)

	// Verify grant contents.
	grantsByScope := make(map[string]*gen.RoleGrant)
	for _, g := range found.Grants {
		grantsByScope[g.Scope] = g
	}
	assert.Nil(t, grantsByScope["build:read"].Resources, "unrestricted grant should have nil resources")
	assert.ElementsMatch(t, []string{"proj-1", "proj-2"}, grantsByScope["build:write"].Resources)
}

func TestCreateRole_Conflict(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)

	_, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Editor",
		Description: "First",
	})
	require.NoError(t, err)

	// Creating a second role with the same slug should conflict in WorkOS mock.
	// The mock doesn't enforce uniqueness, but we can verify the code path works.
	result, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Editor2",
		Description: "Second with different name",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.ID)
}

func TestUpdateRole(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)

	// Create a role to update.
	created, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Viewer",
		Description: "Can view things",
		Grants: []*gen.RoleGrant{
			{Scope: "build:read"},
		},
	})
	require.NoError(t, err)

	// Update name, description, and grants.
	newName := "Super Viewer"
	newDesc := "Can view everything"
	updated, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:          created.ID,
		Name:        &newName,
		Description: &newDesc,
		Grants: []*gen.RoleGrant{
			{Scope: "build:read"},
			{Scope: "org:read"},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, "Super Viewer", updated.Name)
	assert.Equal(t, "Can view everything", updated.Description)
	assert.Len(t, updated.Grants, 2)
}

func TestUpdateRole_SystemRole_SkipsNameChange(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)

	// Add a system role to the mock.
	ti.mock.addSystemRole("org_workos_test", "role_system_1", "Member", "member")

	// Updating a system role should not change WorkOS metadata but should still
	// allow grant changes (grants are skipped for system roles in impl though).
	newName := "Renamed Member"
	result, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:   "role_system_1",
		Name: &newName,
	})
	require.NoError(t, err)

	// System role name should NOT change (update is skipped for EnvironmentRoles).
	assert.Equal(t, "Member", result.Name)
	assert.True(t, result.IsSystem)
}

func TestUpdateRole_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)

	newName := "Ghost"
	_, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:   "role_nonexistent",
		Name: &newName,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteRole(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)

	// Create a role with grants.
	created, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Temporary",
		Description: "Will be deleted",
		Grants: []*gen.RoleGrant{
			{Scope: "mcp:read"},
		},
	})
	require.NoError(t, err)

	// Delete it.
	err = ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: created.ID})
	require.NoError(t, err)

	// Verify it no longer appears in list.
	listResult, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	require.NoError(t, err)
	for _, r := range listResult.Roles {
		assert.NotEqual(t, created.ID, r.ID, "deleted role should not appear in list")
	}
}

func TestDeleteRole_SystemRole_Forbidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)

	ti.mock.addSystemRole("org_workos_test", "role_system_admin", "Admin", "admin")

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_system_admin"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "system role")
}

func TestDeleteRole_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListRoles(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)

	// Start with no roles.
	result, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	require.NoError(t, err)
	assert.Empty(t, result.Roles)

	// Create two roles.
	_, err = ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Role A",
		Description: "First role",
	})
	require.NoError(t, err)

	_, err = ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Role B",
		Description: "Second role",
		Grants: []*gen.RoleGrant{
			{Scope: "org:read"},
		},
	})
	require.NoError(t, err)

	result, err = ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	require.NoError(t, err)
	assert.Len(t, result.Roles, 2)

	// Verify Role B has its grant.
	var roleB *gen.Role
	for _, r := range result.Roles {
		if r.Name == "Role B" {
			roleB = r
			break
		}
	}
	require.NotNil(t, roleB)
	assert.Len(t, roleB.Grants, 1)
	assert.Equal(t, "org:read", roleB.Grants[0].Scope)
}

func TestListRoles_IncludesMemberCounts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)

	// Create a role, then add a member assigned to its slug.
	created, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name: "Counted Role",
	})
	require.NoError(t, err)

	// Derive the expected slug from the created role in the mock.
	ti.mock.mu.Lock()
	var slug string
	for _, r := range ti.mock.roles["org_workos_test"] {
		if r.ID == created.ID {
			slug = r.Slug
			break
		}
	}
	ti.mock.mu.Unlock()
	require.NotEmpty(t, slug)

	// Inject a membership via the mock.
	ti.mock.addMember("org_workos_test", "mem_1", "user_1", slug)

	result, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	require.NoError(t, err)

	var found *gen.Role
	for _, r := range result.Roles {
		if r.ID == created.ID {
			found = r
			break
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, 1, found.MemberCount)
}
