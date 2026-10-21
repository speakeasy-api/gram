package access

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func previewItemByName(t *testing.T, result *gen.DirectoryRoleSetupPreviewResult, name string) *gen.DirectoryRoleSetupPreviewItem {
	t.Helper()
	for _, item := range result.Items {
		if item.SourceName == name {
			return item
		}
	}
	require.Failf(t, "preview item not found", "no item named %q", name)
	return nil
}

func expectWorkOSRoleCreate(ti *testInstance, name string) {
	slug, _ := slugify(name)
	ti.roles.On("CreateRole", mock.Anything, mockidp.MockOrgID, thirdpartyworkos.CreateRoleOpts{
		Name: name, Slug: slug, Description: "",
	}).Return(&thirdpartyworkos.Role{
		ID: "role_" + slug, Name: name, Slug: slug, CreatedAt: mockRoleTimestamp, UpdatedAt: mockRoleTimestamp,
	}, nil).Once()
}

func TestService_PreviewDirectoryRoleSetup_ProposesRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID

	// A live role whose slug matches a group name is reused, with its grants shown.
	engineeringID := seedRole(t, ctx, ti.conn, orgID, mockRole("role_eng", "Engineering", "org-engineering", ""))
	engineering := seededRolePrincipal(t, ctx, ti.conn, orgID, "org-engineering")
	seedGrant(t, ctx, ti.conn, orgID, engineering, authz.ScopeProjectRead, "project-1")
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_viewer", "Viewer", "viewer", ""))
	viewer := seededRolePrincipal(t, ctx, ti.conn, orgID, "viewer").String()

	seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Engineering")
	seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "R&D (EMEA)")
	mappedGroupID := seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Support").String()
	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &mappedGroupID, RoleUrns: []string{viewer},
	})
	require.NoError(t, err)

	seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "sales@test.com", `{"department":"Sales"}`)
	// A key with more distinct values than the cap is hidden, and named.
	for i := range maxDirectoryAttributeOptionsPerKey + 1 {
		seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", fmt.Sprintf("person%d@test.com", i), fmt.Sprintf(`{"employee_id":"E%d"}`, i))
	}

	result, err := ti.service.PreviewDirectoryRoleSetup(ctx, &gen.PreviewDirectoryRoleSetupPayload{})
	require.NoError(t, err)

	reused := previewItemByName(t, result, "Engineering")
	require.Empty(t, reused.ExistingRoleUrns)
	require.Len(t, reused.ProposedRoles, 1)
	require.Equal(t, directoryRoleSetupActionReuse, reused.ProposedRoles[0].RoleAction)
	require.Equal(t, engineeringID, conv.PtrValOr(reused.ProposedRoles[0].RoleID, ""))
	require.Len(t, reused.ProposedRoles[0].CurrentGrants, 1)

	created := previewItemByName(t, result, "R&D (EMEA)")
	require.Equal(t, directoryRoleSetupActionCreate, created.ProposedRoles[0].RoleAction)
	require.Equal(t, "R D EMEA", created.ProposedRoles[0].RoleName)
	require.Empty(t, created.ProposedRoles[0].CurrentGrants)

	mapped := previewItemByName(t, result, "Support")
	require.Equal(t, []string{viewer}, mapped.ExistingRoleUrns)
	require.Equal(t, viewer, conv.PtrValOr(mapped.ProposedRoles[0].RoleUrn, ""))

	attribute := previewItemByName(t, result, "department=Sales")
	require.Equal(t, int64(1), attribute.DirectoryPeople)
	require.Equal(t, "Sales", attribute.ProposedRoles[0].RoleName)

	for _, item := range result.Items {
		require.NotEqual(t, "employee_id", conv.PtrValOr(item.Source.AttributeKey, ""))
	}
	require.Equal(t, int64(maxDirectoryAttributeOptionsPerKey), result.MaxValuesPerAttributeKey)
	require.Len(t, result.HiddenAttributeKeys, 1)
	require.Equal(t, "employee_id", result.HiddenAttributeKeys[0].Key)
	require.Equal(t, int64(maxDirectoryAttributeOptionsPerKey+1), result.HiddenAttributeKeys[0].ValueCount)
}

func TestService_ApplyDirectoryRoleSetup_CreatesRolesAndMappings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID

	viewerID := seedRole(t, ctx, ti.conn, orgID, mockRole("role_viewer", "Viewer", "viewer", ""))
	viewer := seededRolePrincipal(t, ctx, ti.conn, orgID, "viewer").String()
	seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Platform")
	seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "ops@test.com", `{"team":"Platform"}`)

	expectWorkOSRoleCreate(ti, "Platform")

	preview, err := ti.service.PreviewDirectoryRoleSetup(ctx, &gen.PreviewDirectoryRoleSetupPayload{})
	require.NoError(t, err)
	group := previewItemByName(t, preview, "Platform")
	attribute := previewItemByName(t, preview, "team=Platform")

	createName := "Platform"
	grantSet := []*gen.RoleGrant{{Scope: string(authz.ScopeProjectRead), Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-1"}}}}
	result, err := ti.service.ApplyDirectoryRoleSetup(ctx, &gen.ApplyDirectoryRoleSetupPayload{
		Items: []*gen.ApplyDirectoryRoleSetupItem{
			{Source: group.Source, Version: group.Version, Roles: []*gen.ApplyDirectoryRoleSetupRole{{CreateName: &createName}, {RoleUrn: &viewer}}},
			// Both sources name the same new role: it is created once and shared.
			{Source: attribute.Source, Version: attribute.Version, Roles: []*gen.ApplyDirectoryRoleSetupRole{{CreateName: &createName}}},
		},
		GrantSet: grantSet,
	})
	require.NoError(t, err)

	require.Len(t, result.CreatedRoles, 1)
	role := result.CreatedRoles[0]
	require.Equal(t, "Platform", role.Name)
	require.Len(t, role.Grants, 1, "new roles get exactly the grant set")
	require.Len(t, result.Mappings, 3)
	require.Equal(t, []*gen.DirectoryRoleSetupSideEffect{{Kind: "workos_role_sync", RoleID: role.ID, RequestStatus: "requested"}}, result.SideEffects)

	// The existing role keeps its own (empty) grants.
	stored, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: viewerID})
	require.NoError(t, err)
	require.Empty(t, stored.Grants)

	listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	urns := []string{}
	for _, mapping := range listed.Mappings {
		urns = append(urns, mapping.RoleUrn)
	}
	require.ElementsMatch(t, []string{role.PrincipalUrn, viewer, role.PrincipalUrn}, urns)
}

func TestService_ApplyDirectoryRoleSetup_StaleVersionChangesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_viewer", "Viewer", "viewer", ""))
	viewer := seededRolePrincipal(t, ctx, ti.conn, orgID, "viewer").String()
	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Platform").String()
	seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Finance")

	preview, err := ti.service.PreviewDirectoryRoleSetup(ctx, &gen.PreviewDirectoryRoleSetupPayload{})
	require.NoError(t, err)
	platform := previewItemByName(t, preview, "Platform")
	finance := previewItemByName(t, preview, "Finance")

	// Someone maps the group after the preview.
	_, err = ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &groupID, RoleUrns: []string{viewer},
	})
	require.NoError(t, err)

	createName := "Finance"
	_, err = ti.service.ApplyDirectoryRoleSetup(ctx, &gen.ApplyDirectoryRoleSetupPayload{
		Items: []*gen.ApplyDirectoryRoleSetupItem{
			{Source: finance.Source, Version: finance.Version, Roles: []*gen.ApplyDirectoryRoleSetupRole{{CreateName: &createName}}},
			{Source: platform.Source, Version: platform.Version, Roles: []*gen.ApplyDirectoryRoleSetupRole{}},
		},
	})
	requireOopsCode(t, err, oops.CodeConflict)

	// The whole batch was refused: no role created, the new mapping kept.
	roles, err := ti.service.ListRoles(ctx, &gen.ListRolesPayload{})
	require.NoError(t, err)
	for _, role := range roles.Roles {
		require.NotEqual(t, "Finance", role.Name)
	}
	listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Mappings, 1)
}

func TestService_ApplyDirectoryRoleSetup_RejectsMalformedItems(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Platform").String()
	source := &gen.DirectoryRoleSetupSource{SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &groupID}
	name := "Platform"
	roleURN := urn.NewPrincipal(urn.PrincipalTypeRole, "organization:role").String()
	otherGroupID := seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Platform Ops").String()
	otherSource := &gen.DirectoryRoleSetupSource{SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &otherGroupID}
	spaced, dashed := "Platform Ops", "platform-ops"
	tooManyRoles := make([]*gen.ApplyDirectoryRoleSetupRole, 0, maxDirectoryRolesPerSource+1)
	for i := range maxDirectoryRolesPerSource + 1 {
		tooManyRoles = append(tooManyRoles, &gen.ApplyDirectoryRoleSetupRole{CreateName: new(fmt.Sprintf("Role %d", i))})
	}

	for label, items := range map[string][]*gen.ApplyDirectoryRoleSetupItem{
		// Refused before any of the new roles is created.
		"more roles than the cap": {
			{Source: source, Version: "v", Roles: tooManyRoles},
		},
		"duplicate source": {
			{Source: source, Version: "v", Roles: nil},
			{Source: source, Version: "v", Roles: nil},
		},
		"role with both fields": {
			{Source: source, Version: "v", Roles: []*gen.ApplyDirectoryRoleSetupRole{{RoleUrn: &roleURN, CreateName: &name}}},
		},
		"role with neither field": {
			{Source: source, Version: "v", Roles: []*gen.ApplyDirectoryRoleSetupRole{{}}},
		},
		// Different names that would become the same role are refused rather
		// than silently merged.
		"new role names colliding on a slug": {
			{Source: source, Version: "v", Roles: []*gen.ApplyDirectoryRoleSetupRole{{CreateName: &spaced}}},
			{Source: otherSource, Version: "v", Roles: []*gen.ApplyDirectoryRoleSetupRole{{CreateName: &dashed}}},
		},
	} {
		_, err := ti.service.ApplyDirectoryRoleSetup(ctx, &gen.ApplyDirectoryRoleSetupPayload{Items: items})
		require.Error(t, err, label)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestDirectoryRoleName(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"Engineering": "Engineering",
		"R&D (EMEA)":  "R D EMEA",
		"  data_ops ": "data_ops",
		"***":         fallbackDirectoryRoleName,
		// Allowed characters, but no letter or digit to make a role from.
		"-":   fallbackDirectoryRoleName,
		"_ _": fallbackDirectoryRoleName,
	} {
		require.Equal(t, want, directoryRoleName(input), input)
	}
}
