package access

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func seedMappingDirectoryGroup(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, name string) uuid.UUID {
	t.Helper()

	groupID, _ := seedMappingDirectoryGroupWithWorkOSID(t, ctx, conn, orgID, name)
	return groupID
}

func seedMappingDirectoryGroupWithWorkOSID(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, name string) (uuid.UUID, string) {
	t.Helper()

	now := time.Now().UTC()
	workosGroupID := "grp_" + uuid.NewString()
	groupID, err := directoryrepo.New(conn).UpsertDirectoryGroup(ctx, directoryrepo.UpsertDirectoryGroupParams{
		OrganizationID:         orgID,
		WorkosDirectoryGroupID: workosGroupID,
		Name:                   name,
		Attributes:             []byte(`{}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(now),
		WorkosLastEventID:      conv.ToPGText("event_" + workosGroupID),
	})
	require.NoError(t, err)

	return groupID, workosGroupID
}

func deleteMappingDirectoryGroup(t *testing.T, ctx context.Context, conn *pgxpool.Pool, workosGroupID string) {
	t.Helper()

	group, err := directoryrepo.New(conn).GetDirectoryGroupForMembershipByWorkOSID(ctx, workosGroupID)
	require.NoError(t, err)
	_, err = directoryrepo.New(conn).DeleteDirectoryGroupByWorkOSID(ctx, directoryrepo.DeleteDirectoryGroupByWorkOSIDParams{
		OrganizationID:         group.OrganizationID,
		WorkosDeletedAt:        conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID:      conv.ToPGText("event_delete_" + workosGroupID),
		WorkosDirectoryGroupID: workosGroupID,
	})
	require.NoError(t, err)
}

func addMappingGroupMember(t *testing.T, ctx context.Context, conn *pgxpool.Pool, directoryUserID uuid.UUID, workosUserID string, groupID uuid.UUID, workosGroupID string) {
	t.Helper()

	_, err := directoryrepo.New(conn).OpenDirectoryUserGroupMembership(ctx, directoryrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        directoryUserID,
		DirectoryGroupID:       groupID,
		WorkosDirectoryUserID:  workosUserID,
		WorkosDirectoryGroupID: workosGroupID,
		WorkosCreatedAt:        conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)
}

func seedMappingDirectoryUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, userID, email, attributes string) (uuid.UUID, string) {
	t.Helper()

	now := time.Now().UTC()
	workosUserID := "du_" + uuid.NewString()
	directoryUserID, err := directoryrepo.New(conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        orgID,
		UserID:                conv.ToPGTextEmpty(userID),
		WorkosDirectoryUserID: workosUserID,
		Email:                 conv.ToPGText(email),
		Attributes:            []byte(attributes),
		RestoreDeleted:        true,
		WorkosCreatedAt:       conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(now),
		WorkosLastEventID:     conv.ToPGText("event_" + workosUserID),
	})
	require.NoError(t, err)

	return directoryUserID, workosUserID
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func TestService_ListDirectoryRoleMappings_ForbiddenWithOrgReadOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgRead, Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID)})

	_, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_SetDirectoryRoleMapping_GroupCreatesAndReplaces(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_viewer", "Viewer", "viewer", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	viewer := seededRolePrincipal(t, ctx, ti.conn, orgID, "viewer").String()
	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Engineering").String()

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)

	created, err := ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &groupID,
		RoleUrn:          builder,
	})
	require.NoError(t, err)
	require.Equal(t, directoryRoleMappingSourceGroup, created.SourceKind)
	require.Equal(t, groupID, conv.PtrValOr(created.DirectoryGroupID, ""))
	require.Equal(t, "Engineering", conv.PtrValOr(created.DirectoryGroupName, ""))
	require.Equal(t, builder, created.RoleUrn)

	replaced, err := ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &groupID,
		RoleUrn:          viewer,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, replaced.ID)
	require.Equal(t, viewer, replaced.RoleUrn)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Equal(t, before+2, after)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Equal(t, "Engineering", record.SubjectDisplay)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, viewer, metadata["role_urn"])
	require.Equal(t, builder, metadata["previous_role_urn"])

	listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Mappings, 1)
	require.Equal(t, viewer, listed.Mappings[0].RoleUrn)
	require.Len(t, listed.Groups, 1)
	require.Equal(t, "Engineering", listed.Groups[0].Name)
}

func TestService_SetDirectoryRoleMapping_AttributeRequiresKnownValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "sales@test.com", `{"department_name":"Sales"}`)

	key := "department_name"
	unknown := "Marketing"
	_, err := ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &unknown,
		RoleUrn:        builder,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	known := "Sales"
	mapping, err := ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &known,
		RoleUrn:        builder,
	})
	require.NoError(t, err)
	require.Equal(t, directoryRoleMappingSourceAttribute, mapping.SourceKind)
	require.Nil(t, mapping.DirectoryGroupID)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Equal(t, "department_name=Sales", record.SubjectDisplay)
}

func TestService_SetDirectoryRoleMapping_RejectsUnknownRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "Engineering").String()

	_, err := ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &groupID,
		RoleUrn:          "role:organization:" + uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestService_SetDirectoryRoleMapping_RejectsMixedSourceFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	groupID := uuid.NewString()
	key := "department_name"

	_, err := ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &groupID,
		AttributeKey:     &key,
		RoleUrn:          builder,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:   directoryRoleMappingSourceAttribute,
		AttributeKey: &key,
		RoleUrn:      builder,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_DeleteDirectoryRoleMapping(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Engineering").String()

	mapping, err := ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &groupID,
		RoleUrn:          builder,
	})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteDirectoryRoleMapping(ctx, &gen.DeleteDirectoryRoleMappingPayload{ID: mapping.ID}))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingDelete)
	require.NoError(t, err)
	require.Equal(t, mapping.ID, record.SubjectID)
	require.Equal(t, "Engineering", record.SubjectDisplay)

	listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	require.Empty(t, listed.Mappings)

	err = ti.service.DeleteDirectoryRoleMapping(ctx, &gen.DeleteDirectoryRoleMappingPayload{ID: mapping.ID})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestService_SyncDirectoryGroups_SavesGroupsFromLinkedDirectories(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	ti.roles.On("ListDirectories", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Directory{
		{ID: "directory_linked", OrganizationID: mockidp.MockOrgID, Type: "okta scim v2.0", Name: "Okta", State: "linked", CreatedAt: "", UpdatedAt: ""},
		{ID: "directory_unlinked", OrganizationID: mockidp.MockOrgID, Type: "okta scim v2.0", Name: "Old", State: "unlinked", CreatedAt: "", UpdatedAt: ""},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroups", mock.Anything, "directory_linked").Return([]thirdpartyworkos.DirectoryGroup{
		{ID: "directory_group_" + uuid.NewString(), DirectoryID: "directory_linked", OrganizationID: mockidp.MockOrgID, Name: "Engineering", RawAttributes: nil, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z"},
		{ID: "directory_group_" + uuid.NewString(), DirectoryID: "directory_linked", OrganizationID: mockidp.MockOrgID, Name: "Sales", RawAttributes: []byte(`{"id":"sales"}`), CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z"},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroupUsers", mock.Anything, mock.Anything).Return([]thirdpartyworkos.DirectoryUser{}, nil)

	result, err := ti.service.SyncDirectoryGroups(ctx, &gen.SyncDirectoryGroupsPayload{})
	require.NoError(t, err)
	require.Equal(t, 2, result.GroupCount)
	ti.roles.AssertNotCalled(t, "ListDirectoryGroups", mock.Anything, "directory_unlinked")

	listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	names := make([]string, 0, len(listed.Groups))
	for _, group := range listed.Groups {
		names = append(names, group.Name)
	}
	require.Equal(t, []string{"Engineering", "Sales"}, names)
}

func TestService_DirectoryRoleMapping_GrantsRoleToMatchingMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	builderID := seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder")
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_sales_user", "sales@test.com", "Sales User", "user_sales", "membership_sales")
	seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "local_sales_user", "sales@test.com", `{"department_name":"Sales"}`)

	principals, err := authz.ResolveUserPrincipals(ctx, ti.conn, orgID, "local_sales_user")
	require.NoError(t, err)
	require.NotContains(t, principals, builder)

	key := "department_name"
	value := "Sales"
	_, err = ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &value,
		RoleUrn:        builder.String(),
	})
	require.NoError(t, err)

	principals, err = authz.ResolveUserPrincipals(ctx, ti.conn, orgID, "local_sales_user")
	require.NoError(t, err)
	require.Contains(t, principals, builder)

	role, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: builderID})
	require.NoError(t, err)
	require.Equal(t, 1, role.MemberCount)

	// A direct assignment of the same role is counted once, not twice.
	seedRoleAssignment(t, ctx, ti.conn, orgID, "local_sales_user", mockMember(mockidp.MockOrgID, "membership_sales", "user_sales", "builder"))
	role, err = ti.service.GetRole(ctx, &gen.GetRolePayload{ID: builderID})
	require.NoError(t, err)
	require.Equal(t, 1, role.MemberCount)
}

func TestService_SyncDirectoryGroups_KeepsDeletedAndNewerGroups(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	dirQueries := directoryrepo.New(ti.conn)

	_, deletedWorkOSID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Retired")
	deleteMappingDirectoryGroup(t, ctx, ti.conn, deletedWorkOSID)
	_, newerWorkOSID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	newerBefore, err := dirQueries.GetDirectoryGroupSyncStateByWorkOSID(ctx, newerWorkOSID)
	require.NoError(t, err)

	stale := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	later := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	ti.roles.On("ListDirectories", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Directory{
		{ID: "directory_linked", OrganizationID: mockidp.MockOrgID, Type: "okta scim v2.0", Name: "Okta", State: "linked", CreatedAt: "", UpdatedAt: ""},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroups", mock.Anything, "directory_linked").Return([]thirdpartyworkos.DirectoryGroup{
		// Newer than the delete event: only the deleted guard keeps it deleted.
		{ID: deletedWorkOSID, DirectoryID: "directory_linked", OrganizationID: mockidp.MockOrgID, Name: "Retired", RawAttributes: nil, CreatedAt: stale, UpdatedAt: later},
		// Older than the stored row: only the timestamp guard keeps it as is.
		{ID: newerWorkOSID, DirectoryID: "directory_linked", OrganizationID: mockidp.MockOrgID, Name: "Renamed", RawAttributes: nil, CreatedAt: stale, UpdatedAt: stale},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroupUsers", mock.Anything, mock.Anything).Return([]thirdpartyworkos.DirectoryUser{}, nil)

	_, err = ti.service.SyncDirectoryGroups(ctx, &gen.SyncDirectoryGroupsPayload{})
	require.NoError(t, err)

	deleted, err := dirQueries.GetDirectoryGroupByWorkOSID(ctx, deletedWorkOSID)
	require.NoError(t, err)
	require.True(t, deleted.Deleted, "a listing must not restore a group an event deleted")

	newer, err := dirQueries.GetDirectoryGroupByWorkOSID(ctx, newerWorkOSID)
	require.NoError(t, err)
	require.Equal(t, "Engineering", newer.Name, "an older listing must not overwrite a newer row")
	newerAfter, err := dirQueries.GetDirectoryGroupSyncStateByWorkOSID(ctx, newerWorkOSID)
	require.NoError(t, err)
	require.Equal(t, newerBefore.WorkosUpdatedAt, newerAfter.WorkosUpdatedAt)
}

func listedDirectoryUser(workosUserID, email, state string) thirdpartyworkos.DirectoryUser {
	return thirdpartyworkos.DirectoryUser{
		ID:               workosUserID,
		DirectoryID:      "directory_linked",
		OrganizationID:   mockidp.MockOrgID,
		Email:            email,
		State:            state,
		CustomAttributes: nil,
		CreatedAt:        "2026-05-14T17:04:54Z",
		UpdatedAt:        "2026-05-14T17:04:54Z",
	}
}

func countOpenGroupMemberships(t *testing.T, ctx context.Context, conn *pgxpool.Pool, workosGroupID, workosUserID string) int64 {
	t.Helper()

	count, err := directoryrepo.New(conn).CountDirectoryUserGroupMembershipsByWorkOSIDs(ctx, directoryrepo.CountDirectoryUserGroupMembershipsByWorkOSIDsParams{
		WorkosDirectoryGroupID: workosGroupID,
		WorkosDirectoryUserID:  workosUserID,
	})
	require.NoError(t, err)
	return count
}

// The organization's event cursor sat past the events that put most members
// in the group, so only a member added later has a row. Syncing groups reads
// the full member list and fills the rest in.
func TestService_SyncDirectoryGroups_AddsMembersFromBeforeEventCursor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	groupID, workosGroupID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	newcomerID, newcomerWorkOSID := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "newcomer@test.com", `{}`)
	addMappingGroupMember(t, ctx, ti.conn, newcomerID, newcomerWorkOSID, groupID, workosGroupID)
	_, historicalWorkOSID := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "historical@test.com", `{}`)
	unsyncedWorkOSID := "du_" + uuid.NewString()
	inactiveWorkOSID := "du_" + uuid.NewString()

	ti.roles.On("ListDirectories", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Directory{
		{ID: "directory_linked", OrganizationID: mockidp.MockOrgID, Type: "gsuite directory", Name: "Google", State: "linked", CreatedAt: "", UpdatedAt: ""},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroups", mock.Anything, "directory_linked").Return([]thirdpartyworkos.DirectoryGroup{
		{ID: workosGroupID, DirectoryID: "directory_linked", OrganizationID: mockidp.MockOrgID, Name: "Engineering", RawAttributes: nil, CreatedAt: "2026-05-14T17:04:54Z", UpdatedAt: "2026-05-14T17:04:54Z"},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroupUsers", mock.Anything, workosGroupID).Return([]thirdpartyworkos.DirectoryUser{
		listedDirectoryUser(newcomerWorkOSID, "newcomer@test.com", "active"),
		listedDirectoryUser(historicalWorkOSID, "historical@test.com", "active"),
		listedDirectoryUser(unsyncedWorkOSID, "Unsynced@Test.com", "active"),
		listedDirectoryUser(inactiveWorkOSID, "inactive@test.com", "inactive"),
	}, nil).Once()

	_, err := ti.service.SyncDirectoryGroups(ctx, &gen.SyncDirectoryGroupsPayload{})
	require.NoError(t, err)

	require.Equal(t, int64(1), countOpenGroupMemberships(t, ctx, ti.conn, workosGroupID, newcomerWorkOSID))
	require.Equal(t, int64(1), countOpenGroupMemberships(t, ctx, ti.conn, workosGroupID, historicalWorkOSID), "a member with a directory row but no membership event is added")
	require.Equal(t, int64(1), countOpenGroupMemberships(t, ctx, ti.conn, workosGroupID, unsyncedWorkOSID), "a member with no directory row is saved and added")
	require.Equal(t, int64(0), countOpenGroupMemberships(t, ctx, ti.conn, workosGroupID, inactiveWorkOSID), "an inactive member gains nothing")

	unsynced, err := directoryrepo.New(ti.conn).GetDirectoryUserByWorkOSID(ctx, unsyncedWorkOSID)
	require.NoError(t, err)
	require.Equal(t, orgID, unsynced.OrganizationID)
	require.Equal(t, "unsynced@test.com", unsynced.Email.String)
	require.Equal(t, "directory_linked", unsynced.DirectoryID.String, "a listed user is attributed to its directory")

	listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Groups, 1)
	require.Equal(t, int64(3), listed.Groups[0].MemberCount)
}

func TestService_SyncDirectoryGroups_GrantsGroupRoleToMemberFromBeforeEventCursor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder")
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_eng_user", "eng@test.com", "Eng User", "user_eng", "membership_eng")
	_, workosUserID := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "local_eng_user", "eng@test.com", `{}`)
	groupID, workosGroupID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")

	group := groupID.String()
	_, err := ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &group,
		RoleUrn:          builder.String(),
	})
	require.NoError(t, err)

	principals, err := authz.ResolveUserPrincipals(ctx, ti.conn, orgID, "local_eng_user")
	require.NoError(t, err)
	require.NotContains(t, principals, builder, "no membership event reached the group, so the mapping grants nothing yet")

	ti.roles.On("ListDirectories", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Directory{
		{ID: "directory_linked", OrganizationID: mockidp.MockOrgID, Type: "gsuite directory", Name: "Google", State: "linked", CreatedAt: "", UpdatedAt: ""},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroups", mock.Anything, "directory_linked").Return([]thirdpartyworkos.DirectoryGroup{
		{ID: workosGroupID, DirectoryID: "directory_linked", OrganizationID: mockidp.MockOrgID, Name: "Engineering", RawAttributes: nil, CreatedAt: "2026-05-14T17:04:54Z", UpdatedAt: "2026-05-14T17:04:54Z"},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroupUsers", mock.Anything, workosGroupID).Return([]thirdpartyworkos.DirectoryUser{
		listedDirectoryUser(workosUserID, "eng@test.com", "active"),
	}, nil).Once()

	_, err = ti.service.SyncDirectoryGroups(ctx, &gen.SyncDirectoryGroupsPayload{})
	require.NoError(t, err)

	principals, err = authz.ResolveUserPrincipals(ctx, ti.conn, orgID, "local_eng_user")
	require.NoError(t, err)
	require.Contains(t, principals, builder)
}

func TestService_SyncDirectoryGroups_ClosesMembershipsOfUnlistedMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	groupID, workosGroupID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	staleID, staleWorkOSID := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "stale@test.com", `{}`)
	_, err := directoryrepo.New(ti.conn).OpenDirectoryUserGroupMembership(ctx, directoryrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        staleID,
		DirectoryGroupID:       groupID,
		WorkosDirectoryUserID:  staleWorkOSID,
		WorkosDirectoryGroupID: workosGroupID,
		WorkosCreatedAt:        conv.ToPGTimestamptz(time.Now().UTC().Add(-2 * time.Hour)),
	})
	require.NoError(t, err)
	inactiveID, inactiveWorkOSID := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "inactive@test.com", `{}`)
	addMappingGroupMember(t, ctx, ti.conn, inactiveID, inactiveWorkOSID, groupID, workosGroupID)

	ti.roles.On("ListDirectories", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Directory{
		{ID: "directory_linked", OrganizationID: mockidp.MockOrgID, Type: "gsuite directory", Name: "Google", State: "linked", CreatedAt: "", UpdatedAt: ""},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroups", mock.Anything, "directory_linked").Return([]thirdpartyworkos.DirectoryGroup{
		{ID: workosGroupID, DirectoryID: "directory_linked", OrganizationID: mockidp.MockOrgID, Name: "Engineering", RawAttributes: nil, CreatedAt: "2026-05-14T17:04:54Z", UpdatedAt: "2026-05-14T17:04:54Z"},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroupUsers", mock.Anything, workosGroupID).Return([]thirdpartyworkos.DirectoryUser{
		listedDirectoryUser(inactiveWorkOSID, "inactive@test.com", "inactive"),
	}, nil).Once()

	_, err = ti.service.SyncDirectoryGroups(ctx, &gen.SyncDirectoryGroupsPayload{})
	require.NoError(t, err)

	require.Equal(t, int64(0), countOpenGroupMemberships(t, ctx, ti.conn, workosGroupID, staleWorkOSID), "a member missing from the listing is removed")
	require.Equal(t, int64(1), countOpenGroupMemberships(t, ctx, ti.conn, workosGroupID, inactiveWorkOSID), "a listed inactive member keeps its membership")
}

func TestService_SyncDirectoryGroups_FailedMemberListingChangesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	groupID, workosGroupID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	memberID, memberWorkOSID := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "member@test.com", `{}`)
	addMappingGroupMember(t, ctx, ti.conn, memberID, memberWorkOSID, groupID, workosGroupID)

	ti.roles.On("ListDirectories", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Directory{
		{ID: "directory_linked", OrganizationID: mockidp.MockOrgID, Type: "gsuite directory", Name: "Google", State: "linked", CreatedAt: "", UpdatedAt: ""},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroups", mock.Anything, "directory_linked").Return([]thirdpartyworkos.DirectoryGroup{
		{ID: workosGroupID, DirectoryID: "directory_linked", OrganizationID: mockidp.MockOrgID, Name: "Engineering", RawAttributes: nil, CreatedAt: "2026-05-14T17:04:54Z", UpdatedAt: "2026-05-14T17:04:54Z"},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroupUsers", mock.Anything, workosGroupID).Return(nil, fmt.Errorf("workos unavailable")).Once()

	_, err := ti.service.SyncDirectoryGroups(ctx, &gen.SyncDirectoryGroupsPayload{})
	requireOopsCode(t, err, oops.CodeGatewayError)

	require.Equal(t, int64(1), countOpenGroupMemberships(t, ctx, ti.conn, workosGroupID, memberWorkOSID), "a failed listing must not remove members")
}

// A membership event processed while the listing is in flight describes a
// newer state than the listing, so the listing does not undo it.
func TestService_SyncDirectoryGroups_KeepsMembershipRemovedDuringListing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	groupID, workosGroupID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	memberID, memberWorkOSID := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "member@test.com", `{}`)
	addMappingGroupMember(t, ctx, ti.conn, memberID, memberWorkOSID, groupID, workosGroupID)

	// The listing still names the member, but the dsync.group.user_removed
	// event is applied while the listing request is in flight.
	removeDuringListing := func(mock.Arguments) {
		_, err := directoryrepo.New(ti.conn).CloseDirectoryUserGroupMembershipByWorkOSIDs(ctx, directoryrepo.CloseDirectoryUserGroupMembershipByWorkOSIDsParams{
			WorkosCreatedAt:        conv.ToPGTimestamptz(time.Now().UTC()),
			WorkosDirectoryUserID:  memberWorkOSID,
			WorkosDirectoryGroupID: workosGroupID,
		})
		require.NoError(t, err)
	}

	ti.roles.On("ListDirectories", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Directory{
		{ID: "directory_linked", OrganizationID: mockidp.MockOrgID, Type: "gsuite directory", Name: "Google", State: "linked", CreatedAt: "", UpdatedAt: ""},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroups", mock.Anything, "directory_linked").Return([]thirdpartyworkos.DirectoryGroup{
		{ID: workosGroupID, DirectoryID: "directory_linked", OrganizationID: mockidp.MockOrgID, Name: "Engineering", RawAttributes: nil, CreatedAt: "2026-05-14T17:04:54Z", UpdatedAt: "2026-05-14T17:04:54Z"},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroupUsers", mock.Anything, workosGroupID).Run(removeDuringListing).Return([]thirdpartyworkos.DirectoryUser{
		listedDirectoryUser(memberWorkOSID, "member@test.com", "active"),
	}, nil).Once()

	_, err := ti.service.SyncDirectoryGroups(ctx, &gen.SyncDirectoryGroupsPayload{})
	require.NoError(t, err)

	require.Equal(t, int64(0), countOpenGroupMemberships(t, ctx, ti.conn, workosGroupID, memberWorkOSID))
}

// WorkOS directory user ids are global, so a listed id whose row belongs to
// another organization must not join this organization's group.
func TestService_SyncDirectoryGroups_IgnoresDirectoryUserOfAnotherOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	otherOrgID := "org_other_" + uuid.NewString()
	_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       otherOrgID,
		Name:     otherOrgID,
		Slug:     otherOrgID,
		WorkosID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	_, workosGroupID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	_, otherWorkOSUserID := seedMappingDirectoryUser(t, ctx, ti.conn, otherOrgID, "", "other@test.com", `{}`)

	ti.roles.On("ListDirectories", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Directory{
		{ID: "directory_linked", OrganizationID: mockidp.MockOrgID, Type: "gsuite directory", Name: "Google", State: "linked", CreatedAt: "", UpdatedAt: ""},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroups", mock.Anything, "directory_linked").Return([]thirdpartyworkos.DirectoryGroup{
		{ID: workosGroupID, DirectoryID: "directory_linked", OrganizationID: mockidp.MockOrgID, Name: "Engineering", RawAttributes: nil, CreatedAt: "2026-05-14T17:04:54Z", UpdatedAt: "2026-05-14T17:04:54Z"},
	}, nil).Once()
	ti.roles.On("ListDirectoryGroupUsers", mock.Anything, workosGroupID).Return([]thirdpartyworkos.DirectoryUser{
		listedDirectoryUser(otherWorkOSUserID, "other@test.com", "active"),
	}, nil).Once()

	_, err = ti.service.SyncDirectoryGroups(ctx, &gen.SyncDirectoryGroupsPayload{})
	require.NoError(t, err)

	require.Equal(t, int64(0), countOpenGroupMemberships(t, ctx, ti.conn, workosGroupID, otherWorkOSUserID))
	other, err := directoryrepo.New(ti.conn).GetDirectoryUserByWorkOSID(ctx, otherWorkOSUserID)
	require.NoError(t, err)
	require.Equal(t, otherOrgID, other.OrganizationID, "the other organization's row is left as it is")
}

func TestService_DirectoryRoleMapping_GroupMappingFollowsGroupLifecycle(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder")
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_eng_user", "eng@test.com", "Eng User", "user_eng", "membership_eng")
	directoryUserID, workosUserID := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "local_eng_user", "eng@test.com", `{}`)
	groupID, workosGroupID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	addMappingGroupMember(t, ctx, ti.conn, directoryUserID, workosUserID, groupID, workosGroupID)

	group := groupID.String()
	_, err := ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &group,
		RoleUrn:          builder.String(),
	})
	require.NoError(t, err)

	principals, err := authz.ResolveUserPrincipals(ctx, ti.conn, orgID, "local_eng_user")
	require.NoError(t, err)
	require.Contains(t, principals, builder)

	deleteMappingDirectoryGroup(t, ctx, ti.conn, workosGroupID)
	principals, err = authz.ResolveUserPrincipals(ctx, ti.conn, orgID, "local_eng_user")
	require.NoError(t, err)
	require.NotContains(t, principals, builder, "a deleted group grants nothing")
}

func TestService_DirectoryRoleMapping_SkipsDeletedRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder")
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_sales_user", "sales@test.com", "Sales User", "user_sales", "membership_sales")
	seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "local_sales_user", "sales@test.com", `{"department_name":"Sales"}`)

	key := "department_name"
	value := "Sales"
	_, err := ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &value,
		RoleUrn:        builder.String(),
	})
	require.NoError(t, err)

	principals, err := authz.ResolveUserPrincipals(ctx, ti.conn, orgID, "local_sales_user")
	require.NoError(t, err)
	require.Contains(t, principals, builder)

	_, err = repo.New(ti.conn).MarkOrganizationRoleDeletedLocally(ctx, repo.MarkOrganizationRoleDeletedLocallyParams{
		OrganizationID: orgID,
		WorkosSlug:     "builder",
	})
	require.NoError(t, err)

	principals, err = authz.ResolveUserPrincipals(ctx, ti.conn, orgID, "local_sales_user")
	require.NoError(t, err)
	require.NotContains(t, principals, builder, "a mapping to a deleted role grants nothing")
}

func TestService_DirectoryRoleMapping_EmailFallbackIgnoresProfileLinkedToAnotherUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	builderID := seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder")
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_user_a", "a@test.com", "User A", "user_a", "membership_a")
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_user_b", "b@test.com", "User B", "user_b", "membership_b")
	// User A's directory profile carries user B's email address.
	seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "local_user_a", "b@test.com", `{"department_name":"Sales"}`)

	key := "department_name"
	value := "Sales"
	_, err := ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &value,
		RoleUrn:        builder.String(),
	})
	require.NoError(t, err)

	principals, err := authz.ResolveUserPrincipals(ctx, ti.conn, orgID, "local_user_a")
	require.NoError(t, err)
	require.Contains(t, principals, builder)

	principals, err = authz.ResolveUserPrincipals(ctx, ti.conn, orgID, "local_user_b")
	require.NoError(t, err)
	require.NotContains(t, principals, builder, "a profile linked to another user must not match by email")

	role, err := ti.service.GetRole(ctx, &gen.GetRolePayload{ID: builderID})
	require.NoError(t, err)
	require.Equal(t, 1, role.MemberCount)
}

func TestService_ListDirectoryRoleMappings_LeavesOutHighCardinalityAttributeKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	for i := range maxDirectoryAttributeOptionsPerKey + 1 {
		email := fmt.Sprintf("person%d@test.com", i)
		seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", email, fmt.Sprintf(`{"department_name":"Sales","employee_id":"E%d"}`, i))
	}

	listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Attributes, 1)
	require.Equal(t, "department_name", listed.Attributes[0].Key)
	require.Equal(t, "Sales", listed.Attributes[0].Value)

	// The cap only trims the options: an existing value can still be mapped.
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	key := "employee_id"
	value := "E7"
	_, err = ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &value,
		RoleUrn:        builder,
	})
	require.NoError(t, err)
}

func TestService_ListAudienceOptions_CountsDirectoryMappedRoleMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_sales_user", "sales@test.com", "Sales User", "user_sales", "membership_sales")
	seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "local_sales_user", "sales@test.com", `{"department_name":"Sales"}`)

	key := "department_name"
	value := "Sales"
	_, err := ti.service.SetDirectoryRoleMapping(ctx, &gen.SetDirectoryRoleMappingPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &value,
		RoleUrn:        builder,
	})
	require.NoError(t, err)

	options, err := ti.service.ListAudienceOptions(ctx, &gen.ListAudienceOptionsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	var count *int64
	for _, option := range options.Options {
		if option.PrincipalUrn == builder {
			count = option.MemberCount
		}
	}
	require.Equal(t, new(int64(1)), count)
}
