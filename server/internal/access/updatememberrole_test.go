package access

import (
	"errors"
	"sort"
	"testing"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestService_UpdateMemberRoles_SingleRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "admin"))
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{"custom-builder"}).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlugs:      []string{"custom-builder"},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	member, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{builderID}})
	require.NoError(t, err)
	require.Equal(t, "local_user_1", member.ID)
	require.Equal(t, "Ada Lovelace", member.Name)
	require.Equal(t, "ada@example.com", member.Email)
	require.Equal(t, []string{builderID}, member.RoleIds)
	require.Nil(t, member.PhotoURL)
	require.NotEmpty(t, member.JoinedAt)
}

func TestService_UpdateMemberRoles_MultipleRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	adminID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "admin"))
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", mock.Anything).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlugs:      []string{"admin", "custom-builder"},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	member, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{adminID, builderID}})
	require.NoError(t, err)
	require.Equal(t, "local_user_1", member.ID)

	got := make([]string, len(member.RoleIds))
	copy(got, member.RoleIds)
	sort.Strings(got)
	want := []string{adminID, builderID}
	sort.Strings(want)
	require.Equal(t, want, got)
}

func TestService_UpdateMemberRoles_ReplacesAllExistingRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	adminID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	viewerID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_viewer", "Viewer", "custom-viewer", ""))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	// Seed user with admin + builder roles initially.
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMemberMultiRole("", "membership_1", "user_1", "admin", "custom-builder"))

	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", mock.Anything).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlugs:      []string{"custom-viewer"},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	// Replace both existing roles with just viewer.
	member, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{viewerID}})
	require.NoError(t, err)
	require.Equal(t, []string{viewerID}, member.RoleIds)

	// Verify before snapshot had both old roles.
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)

	beforeIDs, ok := beforeSnapshot["RoleIds"].([]any)
	require.True(t, ok)
	require.Len(t, beforeIDs, 2, "before snapshot should contain 2 previous role IDs")

	// Verify both old IDs are in the before snapshot (order may vary).
	beforeStrs := make([]string, len(beforeIDs))
	for i, v := range beforeIDs {
		s, ok := v.(string)
		require.True(t, ok)
		beforeStrs[i] = s
	}
	sort.Strings(beforeStrs)
	wantBefore := []string{adminID, builderID}
	sort.Strings(wantBefore)
	require.Equal(t, wantBefore, beforeStrs)

	// Verify after snapshot has only the new role.
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, []any{viewerID}, afterSnapshot["RoleIds"])

	// Suppress unused variable warnings.
	_ = adminID
	_ = builderID
}

func TestService_UpdateMemberRoles_RoleNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{"00000000-0000-0000-0000-000000000001"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role not found")
}

func TestService_UpdateMemberRoles_OneInvalidRoleIDFailsAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))

	// One valid, one invalid — should fail entirely.
	_, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{
		UserID:  "local_user_1",
		RoleIds: []string{builderID, "00000000-0000-0000-0000-000000000099"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role not found")
}

func TestService_UpdateMemberRoles_MemberNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	_, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "user_missing", RoleIds: []string{builderID}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "member has not joined this organization")
}

func TestService_UpdateMemberRoles_WorkOSMembershipNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "")

	_, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{builderID}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "member is missing local WorkOS membership linkage")
}

func TestService_UpdateMemberRoles_WorkOSFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "admin"))
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{"custom-builder"}).Return((*thirdpartyworkos.Member)(nil), errors.New("workos unavailable")).Times(3)

	member, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{builderID}})
	require.NoError(t, err)
	require.Equal(t, []string{builderID}, member.RoleIds)
}

func TestService_UpdateMemberRoles_MultipleRolesWorkOSFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	adminID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "admin"))
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", mock.Anything).Return((*thirdpartyworkos.Member)(nil), errors.New("workos unavailable")).Times(3)

	// Local DB should succeed even when WorkOS fails.
	member, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{adminID, builderID}})
	require.NoError(t, err)

	got := make([]string, len(member.RoleIds))
	copy(got, member.RoleIds)
	sort.Strings(got)
	want := []string{adminID, builderID}
	sort.Strings(want)
	require.Equal(t, want, got)
}

func TestService_UpdateMemberRoles_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)

	adminID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "admin"))
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{"custom-builder"}).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlugs:      []string{"custom-builder"},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	member, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{builderID}})
	require.NoError(t, err)
	require.NotNil(t, member)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionAccessMemberRoleUpdate), record.Action)
	require.Equal(t, "access_member", record.SubjectType)
	require.Equal(t, "Ada Lovelace", record.SubjectDisplay)
	require.Equal(t, "ada@example.com", record.SubjectSlug)
	require.NotNil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, []any{adminID}, beforeSnapshot["RoleIds"])
	require.Equal(t, []any{builderID}, afterSnapshot["RoleIds"])

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestService_UpdateMemberRoles_EmptyRoleIds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least one role is required")
}
