package access

import (
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The audit rows for role and membership changes name the sensitive scopes
// the change added, so a reviewer can find who was given access to other
// members' activity without diffing snapshots.

func TestService_CreateRole_AuditLog_SensitiveScopesAdded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	ti.roles.On("CreateRole", mock.Anything, mockidp.MockOrgID, thirdpartyworkos.CreateRoleOpts{
		Name:        "Audit Reviewer",
		Slug:        "org-audit-reviewer",
		Description: "Reads tool logs",
	}).Return(&thirdpartyworkos.Role{
		ID:          "role_audit_reviewer",
		Name:        "Audit Reviewer",
		Slug:        "org-audit-reviewer",
		Description: "Reads tool logs",
		CreatedAt:   mockRoleTimestamp,
		UpdatedAt:   mockRoleTimestamp,
	}, nil).Once()

	role, err := ti.service.CreateRole(ctx, &gen.CreateRolePayload{
		Name:        "Audit Reviewer",
		Description: conv.PtrEmpty("Reads tool logs"),
		Grants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeProjectRead), Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-1"}}},
			{Scope: string(authz.ScopeLogRead), Selectors: []*gen.Selector{{ResourceKind: "project", ResourceID: "project-1"}}},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, role)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleCreate)
	require.NoError(t, err)
	require.Equal(t, role.ID, record.SubjectID)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{string(authz.ScopeLogRead)}, metadata["sensitive_scopes_added"])
}

func TestService_UpdateRole_AuditLog_SensitiveScopesAdded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	roleID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_reviewer", "Reviewer", "custom-reviewer", "Reads sessions"))
	ti.roles.On("UpdateRole", mock.Anything, mockidp.MockOrgID, "custom-reviewer", mock.Anything).Return(&thirdpartyworkos.Role{
		ID:          "role_reviewer",
		Name:        "Reviewer",
		Slug:        "custom-reviewer",
		Description: "Reads sessions",
		CreatedAt:   mockRoleTimestamp,
		UpdatedAt:   mockRoleTimestamp,
	}, nil).Maybe()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+roleID), authz.ScopeChatRead, "project-1")

	// Widening a scope the role already holds in part reports only what is
	// new: chat:write, not the chat:read it already had.
	updated, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID: roleID,
		AddGrants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeChatWrite), Selectors: nil},
			{Scope: string(authz.ScopeProjectRead), Selectors: nil},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, roleID, record.SubjectID)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{string(authz.ScopeChatWrite)}, metadata["sensitive_scopes_added"])

	// Taking access away is not an addition, so the row carries no metadata.
	updated, err = ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID: roleID,
		RemoveGrants: []*gen.RoleGrant{
			{Scope: string(authz.ScopeChatWrite), Selectors: nil},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	record, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Nil(t, record.Metadata)
}

func TestService_UpdateMemberRoles_AuditLog_SensitiveScopesAdded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	builderID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	reviewerID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_reviewer", "Reviewer", "custom-reviewer", ""))
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+reviewerID), authz.ScopeLogRead, "project-1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "custom-builder"))

	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", mock.Anything).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlugs:      []string{"custom-builder", "custom-reviewer"},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Maybe()

	member, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{builderID, reviewerID}})
	require.NoError(t, err)
	require.NotNil(t, member)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, "local_user_1", record.SubjectID)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{string(authz.ScopeLogRead)}, metadata["sensitive_scopes_added"])

	// Dropping back to the builder role adds nothing sensitive.
	member, err = ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{builderID}})
	require.NoError(t, err)
	require.NotNil(t, member)
	record, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Nil(t, record.Metadata)
}
