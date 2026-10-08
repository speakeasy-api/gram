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
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Each service fixture has a private database clone. Simulate the later index
// removal here so this writer can be proved before the rollout enables role sets.
func enableDirectoryRoleSetsForTest(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	//nolint:glint // notestingrawsql: schema-only rollout simulation in a private test database; never expose destructive DDL through production SQLc methods.
	_, err := ti.conn.Exec(ctx, `DROP INDEX IF EXISTS directory_role_mappings_org_group_key;
		DROP INDEX IF EXISTS directory_role_mappings_org_attribute_key;`)
	require.NoError(t, err)
}

func installLegacyDirectoryMappingIndexesForTest(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	//nolint:glint // notestingrawsql: simulate the legacy rollout schema in a private test database, including after those indexes leave schema.sql.
	_, err := ti.conn.Exec(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS directory_role_mappings_org_group_key
		ON directory_role_mappings (organization_id, directory_group_id)
		WHERE deleted IS FALSE AND directory_group_id IS NOT NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS directory_role_mappings_org_attribute_key
		ON directory_role_mappings (organization_id, attribute_key, attribute_value)
		WHERE deleted IS FALSE AND attribute_key IS NOT NULL;`)
	require.NoError(t, err)
}

func seedMappingAdministrator(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	ac := testAccessAuthContext(t, ctx)
	seedGrant(t, ctx, ti.conn, ac.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), authz.ScopeOrgAdmin, ac.ActiveOrganizationID)
}

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

	_, err := directoryrepo.New(conn).DeleteDirectoryGroupByWorkOSID(ctx, directoryrepo.DeleteDirectoryGroupByWorkOSIDParams{
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

func TestService_SetDirectoryRoleMappings_GroupCreatesAndReplaces(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
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

	created, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &groupID,
		RoleUrns:         []string{builder},
	})
	require.NoError(t, err)
	require.Equal(t, directoryRoleMappingSourceGroup, created[0].SourceKind)
	require.Equal(t, groupID, conv.PtrValOr(created[0].DirectoryGroupID, ""))
	require.Equal(t, "Engineering", conv.PtrValOr(created[0].DirectoryGroupName, ""))
	require.Equal(t, builder, created[0].RoleUrn)

	replaced, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &groupID,
		RoleUrns:         []string{viewer},
	})
	require.NoError(t, err)
	require.NotEqual(t, created[0].ID, replaced[0].ID)
	require.Equal(t, viewer, replaced[0].RoleUrn)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Equal(t, before+2, after)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Equal(t, "Engineering", record.SubjectDisplay)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, viewer, metadata["role_urn"])
	require.NotContains(t, metadata, "previous_role_urn")
	removed, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingDelete)
	require.NoError(t, err)
	require.Equal(t, created[0].ID, removed.SubjectID)
	removedMetadata, err := audittest.DecodeAuditData(removed.Metadata)
	require.NoError(t, err)
	require.Equal(t, builder, removedMetadata["role_urn"])

	listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Mappings, 1)
	require.Equal(t, viewer, listed.Mappings[0].RoleUrn)
	require.Len(t, listed.Groups, 1)
	require.Equal(t, "Engineering", listed.Groups[0].Name)
}

func TestService_SetDirectoryRoleMappings_AttributeRequiresKnownValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "sales@test.com", `{"department_name":"Sales"}`)

	key := "department_name"
	unknown := "Marketing"
	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &unknown,
		RoleUrns:       []string{builder},
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	known := "Sales"
	mapping, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &known,
		RoleUrns:       []string{builder},
	})
	require.NoError(t, err)
	require.Equal(t, directoryRoleMappingSourceAttribute, mapping[0].SourceKind)
	require.Nil(t, mapping[0].DirectoryGroupID)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Equal(t, "department_name=Sales", record.SubjectDisplay)
}

func TestService_SetDirectoryRoleMappings_UnchangedAndEmptySets(t *testing.T) {
	t.Parallel()
	for _, sourceKind := range []string{directoryRoleMappingSourceGroup, directoryRoleMappingSourceAttribute} {
		t.Run(sourceKind, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			seedMappingAdministrator(t, ctx, ti)
			orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
			seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
			role := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
			payload := &gen.SetDirectoryRoleMappingsPayload{SourceKind: sourceKind, RoleUrns: []string{role, role}}
			if sourceKind == directoryRoleMappingSourceGroup {
				payload.DirectoryGroupID = conv.PtrEmpty(seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Engineering").String())
			} else {
				seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "", "person@example.test", `{"department":"Engineering"}`)
				payload.AttributeKey, payload.AttributeValue = conv.PtrEmpty("department"), conv.PtrEmpty("Engineering")
			}
			created, err := ti.service.SetDirectoryRoleMappings(ctx, payload)
			require.NoError(t, err)
			require.Len(t, created, 1)
			before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
			require.NoError(t, err)
			unchanged, err := ti.service.SetDirectoryRoleMappings(ctx, payload)
			require.NoError(t, err)
			require.Equal(t, created, unchanged)
			after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
			require.NoError(t, err)
			require.Equal(t, before, after)

			beforeDelete, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingDelete)
			require.NoError(t, err)
			payload.RoleUrns = []string{}
			removed, err := ti.service.SetDirectoryRoleMappings(ctx, payload)
			require.NoError(t, err)
			require.NotNil(t, removed)
			require.Empty(t, removed)
			listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
			require.NoError(t, err)
			require.Empty(t, listed.Mappings)
			afterDelete, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingDelete)
			require.NoError(t, err)
			require.Equal(t, beforeDelete+1, afterDelete)
			_, err = ti.service.SetDirectoryRoleMappings(ctx, payload)
			require.NoError(t, err)
			finalDelete, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingDelete)
			require.NoError(t, err)
			require.Equal(t, afterDelete, finalDelete)
		})
	}
}

func TestService_SetDirectoryRoleMappings_MultipleRoles(t *testing.T) {
	t.Parallel()
	for _, sourceKind := range []string{directoryRoleMappingSourceGroup, directoryRoleMappingSourceAttribute} {
		t.Run(sourceKind, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			seedMappingAdministrator(t, ctx, ti)
			orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
			seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
			seedRole(t, ctx, ti.conn, orgID, mockRole("role_viewer", "Viewer", "viewer", ""))
			builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder")
			viewer := seededRolePrincipal(t, ctx, ti.conn, orgID, "viewer")
			const userID = "directory_member"
			seedConnectedUser(t, ctx, ti.conn, orgID, userID, "person@example.test", "Directory Member", "user_directory_member", "membership_directory_member")
			directoryUserID, workosUserID := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, userID, "person@example.test", `{"department":"Engineering"}`)
			payload := &gen.SetDirectoryRoleMappingsPayload{SourceKind: sourceKind, RoleUrns: []string{viewer.String(), builder.String(), viewer.String()}}
			if sourceKind == directoryRoleMappingSourceGroup {
				groupID, workosGroupID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
				addMappingGroupMember(t, ctx, ti.conn, directoryUserID, workosUserID, groupID, workosGroupID)
				payload.DirectoryGroupID = conv.PtrEmpty(groupID.String())
			} else {
				payload.AttributeKey, payload.AttributeValue = conv.PtrEmpty("department"), conv.PtrEmpty("Engineering")
			}
			installLegacyDirectoryMappingIndexesForTest(t, ctx, ti)
			beforeSet, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
			require.NoError(t, err)
			_, err = ti.service.SetDirectoryRoleMappings(ctx, payload)
			requireOopsCode(t, err, oops.CodeConflict)
			beforeRollout, err := repo.New(ti.conn).ListDirectoryRoleMappings(ctx, orgID)
			require.NoError(t, err)
			require.Empty(t, beforeRollout, "the pre-rollout conflict rolls back the complete set")
			enableDirectoryRoleSetsForTest(t, ctx, ti)
			created, err := ti.service.SetDirectoryRoleMappings(ctx, payload)
			require.NoError(t, err)
			require.Len(t, created, 2)
			listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
			require.NoError(t, err)
			require.ElementsMatch(t, created, listed.Mappings)
			principals, err := authz.ResolveUserPrincipals(ctx, ti.conn, orgID, userID)
			require.NoError(t, err)
			require.Contains(t, principals, builder)
			require.Contains(t, principals, viewer)
			sources, err := repo.New(ti.conn).ListUserDirectoryRoleMappingSources(ctx, repo.ListUserDirectoryRoleMappingSourcesParams{OrganizationID: orgID, UserID: userID})
			require.NoError(t, err)
			require.Len(t, sources, 2)
			for _, source := range sources {
				require.Equal(t, sourceKind, source.SourceKind)
			}
			counts, err := repo.New(ti.conn).ListDirectoryMappedRoleMemberCounts(ctx, repo.ListDirectoryMappedRoleMemberCountsParams{OrganizationID: orgID, RoleUrns: []string{builder.String(), viewer.String()}})
			require.NoError(t, err)
			require.Len(t, counts, 2)
			for _, count := range counts {
				require.EqualValues(t, 1, count.MemberCount)
			}
			unchanged, err := ti.service.SetDirectoryRoleMappings(ctx, payload)
			require.NoError(t, err)
			require.Equal(t, created, unchanged)
			afterSet, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
			require.NoError(t, err)
			require.Equal(t, beforeSet+2, afterSet)
			beforeDelete, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingDelete)
			require.NoError(t, err)
			payload.RoleUrns = []string{}
			removed, err := ti.service.SetDirectoryRoleMappings(ctx, payload)
			require.NoError(t, err)
			require.Empty(t, removed)
			afterDelete, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingDelete)
			require.NoError(t, err)
			require.Equal(t, beforeDelete+2, afterDelete)
		})
	}
}

func TestService_SetDirectoryRoleMapping_LegacyWrapper(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_viewer", "Viewer", "viewer", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	viewer := seededRolePrincipal(t, ctx, ti.conn, orgID, "viewer").String()
	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Engineering").String()
	legacy := &gen.SetDirectoryRoleMappingPayload{SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &groupID, RoleUrn: builder}
	created, err := ti.service.SetDirectoryRoleMapping(ctx, legacy)
	require.NoError(t, err)
	require.Equal(t, builder, created.RoleUrn)
	legacy.RoleUrn = viewer
	replaced, err := ti.service.SetDirectoryRoleMapping(ctx, legacy)
	require.NoError(t, err)
	require.Equal(t, viewer, replaced.RoleUrn)
	enableDirectoryRoleSetsForTest(t, ctx, ti)
	fullSet, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &groupID, RoleUrns: []string{builder, viewer},
	})
	require.NoError(t, err)
	_, err = ti.service.SetDirectoryRoleMapping(ctx, legacy)
	requireOopsCode(t, err, oops.CodeConflict)
	listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	require.ElementsMatch(t, fullSet, listed.Mappings)
}

func TestService_SetDirectoryRoleMappings_RequiresLiveAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, testAccessAuthContext(t, ctx).ActiveOrganizationID, "Engineering").String()
	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &groupID, RoleUrns: []string{},
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_SetDirectoryRoleMappings_InvalidSetPreservesCurrent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	role := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Engineering").String()
	payload := &gen.SetDirectoryRoleMappingsPayload{SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &groupID, RoleUrns: []string{role}}
	created, err := ti.service.SetDirectoryRoleMappings(ctx, payload)
	require.NoError(t, err)
	payload.RoleUrns = []string{"role:organization:" + uuid.NewString()}
	_, err = ti.service.SetDirectoryRoleMappings(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
	listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	require.Equal(t, created, listed.Mappings)
}

func TestService_SetDirectoryRoleMappings_RejectsUnknownRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "Engineering").String()

	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &groupID,
		RoleUrns:         []string{"role:organization:" + uuid.NewString()},
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestService_SetDirectoryRoleMappings_RejectsOtherOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	otherOrgID := "other-" + uuid.NewString()
	seedOrganization(t, ctx, ti.conn, otherOrgID)
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	seedRole(t, ctx, ti.conn, otherOrgID, mockRole("role_viewer", "Viewer", "viewer", ""))
	role := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	otherRole := seededRolePrincipal(t, ctx, ti.conn, otherOrgID, "viewer").String()
	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Engineering").String()
	otherGroupID := seedMappingDirectoryGroup(t, ctx, ti.conn, otherOrgID, "Engineering").String()
	for _, payload := range []*gen.SetDirectoryRoleMappingsPayload{
		{SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &groupID, RoleUrns: []string{otherRole}},
		{SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &otherGroupID, RoleUrns: []string{role}},
		{SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &otherGroupID, RoleUrns: []string{}},
	} {
		_, err := ti.service.SetDirectoryRoleMappings(ctx, payload)
		requireOopsCode(t, err, oops.CodeNotFound)
	}
	seedMappingDirectoryUser(t, ctx, ti.conn, otherOrgID, "", "person@example.test", `{"department":"Other"}`)
	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind: directoryRoleMappingSourceAttribute, AttributeKey: conv.PtrEmpty("department"), AttributeValue: conv.PtrEmpty("Other"), RoleUrns: []string{role},
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	mappings, err := repo.New(ti.conn).ListDirectoryRoleMappings(ctx, orgID)
	require.NoError(t, err)
	require.Empty(t, mappings)
	mappings, err = repo.New(ti.conn).ListDirectoryRoleMappings(ctx, otherOrgID)
	require.NoError(t, err)
	require.Empty(t, mappings)
}

func TestService_SetDirectoryRoleMappings_ClearsDeletedGroup(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	role := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	groupUUID, workosID := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	groupID := groupUUID.String()
	payload := &gen.SetDirectoryRoleMappingsPayload{SourceKind: directoryRoleMappingSourceGroup, DirectoryGroupID: &groupID, RoleUrns: []string{role}}
	_, err := ti.service.SetDirectoryRoleMappings(ctx, payload)
	require.NoError(t, err)
	deleteMappingDirectoryGroup(t, ctx, ti.conn, workosID)
	_, err = ti.service.SetDirectoryRoleMappings(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
	payload.RoleUrns = []string{}
	removed, err := ti.service.SetDirectoryRoleMappings(ctx, payload)
	require.NoError(t, err)
	require.Empty(t, removed)
}

func TestService_SetDirectoryRoleMappings_RejectsMixedSourceFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	groupID := uuid.NewString()
	key := "department_name"

	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &groupID,
		AttributeKey:     &key,
		RoleUrns:         []string{builder},
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:   directoryRoleMappingSourceAttribute,
		AttributeKey: &key,
		RoleUrns:     []string{builder},
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_DeleteDirectoryRoleMapping(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	groupID := seedMappingDirectoryGroup(t, ctx, ti.conn, orgID, "Engineering").String()

	mapping, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &groupID,
		RoleUrns:         []string{builder},
	})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteDirectoryRoleMapping(ctx, &gen.DeleteDirectoryRoleMappingPayload{ID: mapping[0].ID}))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingDelete)
	require.NoError(t, err)
	require.Equal(t, mapping[0].ID, record.SubjectID)
	require.Equal(t, "Engineering", record.SubjectDisplay)

	listed, err := ti.service.ListDirectoryRoleMappings(ctx, &gen.ListDirectoryRoleMappingsPayload{})
	require.NoError(t, err)
	require.Empty(t, listed.Mappings)

	err = ti.service.DeleteDirectoryRoleMapping(ctx, &gen.DeleteDirectoryRoleMappingPayload{ID: mapping[0].ID})
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
	seedMappingAdministrator(t, ctx, ti)
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
	_, err = ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &value,
		RoleUrns:       []string{builder.String()},
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

func TestService_DirectoryRoleMapping_GroupMappingFollowsGroupLifecycle(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
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
	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:       directoryRoleMappingSourceGroup,
		DirectoryGroupID: &group,
		RoleUrns:         []string{builder.String()},
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
	seedMappingAdministrator(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder")
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_sales_user", "sales@test.com", "Sales User", "user_sales", "membership_sales")
	seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "local_sales_user", "sales@test.com", `{"department_name":"Sales"}`)

	key := "department_name"
	value := "Sales"
	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &value,
		RoleUrns:       []string{builder.String()},
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
	seedMappingAdministrator(t, ctx, ti)
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
	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &value,
		RoleUrns:       []string{builder.String()},
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
	seedMappingAdministrator(t, ctx, ti)
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
	_, err = ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &value,
		RoleUrns:       []string{builder},
	})
	require.NoError(t, err)
}

func TestService_ListAudienceOptions_CountsDirectoryMappedRoleMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	seedMappingAdministrator(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_sales_user", "sales@test.com", "Sales User", "user_sales", "membership_sales")
	seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "local_sales_user", "sales@test.com", `{"department_name":"Sales"}`)

	key := "department_name"
	value := "Sales"
	_, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{
		SourceKind:     directoryRoleMappingSourceAttribute,
		AttributeKey:   &key,
		AttributeValue: &value,
		RoleUrns:       []string{builder},
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
