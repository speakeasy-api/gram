package access

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func seedMappingDirectoryGroup(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, name string) uuid.UUID {
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

	return groupID
}

func seedMappingDirectoryUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, userID, email, attributes string) {
	t.Helper()

	now := time.Now().UTC()
	workosUserID := "du_" + uuid.NewString()
	_, err := directoryrepo.New(conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
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
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
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

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
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
}
