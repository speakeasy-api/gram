package access

import (
	"errors"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestService_UpdateMemberRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "admin"))
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", "custom-builder").Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       "custom-builder",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	member, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: builderID})
	require.NoError(t, err)
	require.Equal(t, "local_user_1", member.ID)
	require.Equal(t, "Ada Lovelace", member.Name)
	require.Equal(t, "ada@example.com", member.Email)
	require.Equal(t, builderID, member.RoleID)
	require.Nil(t, member.PhotoURL)
	require.NotEmpty(t, member.JoinedAt)
}

func TestService_UpdateMemberRole_RoleNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: "00000000-0000-0000-0000-000000000001"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "role not found")
}

func TestService_UpdateMemberRole_MemberNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "user_missing", RoleID: builderID})
	require.Error(t, err)
	require.Contains(t, err.Error(), "member has not joined this organization")
}

func TestService_UpdateMemberRole_WorkOSMembershipNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: builderID})
	require.Error(t, err)
	require.Contains(t, err.Error(), "member not found")
}

func TestService_UpdateMemberRole_WorkOSFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "admin"))
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", "custom-builder").Return((*thirdpartyworkos.Member)(nil), errors.New("workos unavailable")).Once()

	_, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: builderID})
	require.Error(t, err)
	require.Contains(t, err.Error(), "update member role in workos")
}

func TestService_UpdateMemberRole_AuditLog(t *testing.T) {
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
	ti.roles.On("UpdateMemberRole", mock.Anything, "membership_1", "custom-builder").Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlug:       "custom-builder",
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Once()

	member, err := ti.service.UpdateMemberRole(ctx, &gen.UpdateMemberRolePayload{UserID: "local_user_1", RoleID: builderID})
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
	require.Equal(t, adminID, beforeSnapshot["RoleID"])
	require.Equal(t, builderID, afterSnapshot["RoleID"])

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}
