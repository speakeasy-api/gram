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
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

func TestService_UpdateRole_AuditLog_SensitiveScopesAdded_NewHolders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_builder", "Builder", "custom-builder", ""))
	reviewerID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_reviewer", "Reviewer", "custom-reviewer", ""))
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+reviewerID), authz.ScopeLogRead, "project-1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "custom-builder"))

	ti.roles.On("UpdateRole", mock.Anything, mockidp.MockOrgID, "custom-reviewer", mock.Anything).Return(&thirdpartyworkos.Role{
		ID:          "role_reviewer",
		Name:        "Reviewer",
		Slug:        "custom-reviewer",
		Description: "",
		CreatedAt:   mockRoleTimestamp,
		UpdatedAt:   mockRoleTimestamp,
	}, nil).Maybe()
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", mock.Anything).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlugs:      []string{"custom-builder", "custom-reviewer"},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Maybe()

	// The role's grants do not change, but the member it is given to did not
	// hold log:read before, so the row names what they gain.
	updated, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:        reviewerID,
		MemberIds: []string{"local_user_1"},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, reviewerID, record.SubjectID)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{string(authz.ScopeLogRead)}, metadata["sensitive_scopes_added"])

	// Saving the same membership again gives nobody anything new.
	updated, err = ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:        reviewerID,
		MemberIds: []string{"local_user_1"},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	record, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Nil(t, record.Metadata)
}

func TestService_UpdateRole_AuditLog_SensitiveScopesAdded_NewHolderWidened(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	scopedID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_scoped", "Scoped", "custom-scoped", ""))
	wideID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_wide", "Wide", "custom-wide", ""))
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+scopedID), authz.ScopeLogRead, "project-1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+wideID), authz.ScopeLogRead, "*")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "custom-scoped"))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "grace@example.com", "Grace Hopper", "user_2", "membership_2")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", mockMember("", "membership_2", "user_2", "custom-wide"))

	ti.roles.On("UpdateRole", mock.Anything, mockidp.MockOrgID, mock.Anything, mock.Anything).Return(&thirdpartyworkos.Role{
		ID:          "role_wide",
		Name:        "Wide",
		Slug:        "custom-wide",
		Description: "",
		CreatedAt:   mockRoleTimestamp,
		UpdatedAt:   mockRoleTimestamp,
	}, nil).Maybe()
	ti.roles.On("UpdateMemberRoles", mock.Anything, mock.Anything, mock.Anything).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlugs:      []string{"custom-scoped", "custom-wide"},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Maybe()

	// A member who reads one project's tool logs is given every project's:
	// the row names log:read although the scope itself is not new to them.
	updated, err := ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:        wideID,
		MemberIds: []string{"local_user_1"},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, wideID, record.SubjectID)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{string(authz.ScopeLogRead)}, metadata["sensitive_scopes_added"])

	// The other way round narrows nothing and widens nothing.
	updated, err = ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{
		ID:        scopedID,
		MemberIds: []string{"local_user_2"},
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	record, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, scopedID, record.SubjectID)
	require.Nil(t, record.Metadata)
}

func TestService_UpdateMemberRoles_AuditLog_SensitiveScopesWidened(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	scopedID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_scoped", "Scoped", "custom-scoped", ""))
	wideID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_wide", "Wide", "custom-wide", ""))
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+scopedID), authz.ScopeLogRead, "project-1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+wideID), authz.ScopeLogRead, "*")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "custom-scoped"))

	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", mock.Anything).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlugs:      []string{"custom-scoped", "custom-wide"},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Maybe()

	// The member snapshots carry only role ids, so the row has to say that
	// log:read now reaches every project.
	member, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{scopedID, wideID}})
	require.NoError(t, err)
	require.NotNil(t, member)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, "local_user_1", record.SubjectID)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{string(authz.ScopeLogRead)}, metadata["sensitive_scopes_added"])

	// Keeping the unrestricted role, the scoped one adds nothing either way.
	member, err = ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{wideID}})
	require.NoError(t, err)
	require.NotNil(t, member)
	record, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Nil(t, record.Metadata)
	member, err = ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{wideID, scopedID}})
	require.NoError(t, err)
	require.NotNil(t, member)
	record, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Nil(t, record.Metadata)
}

func TestService_UpdateMemberRoles_AuditLog_SensitiveScopesWidened_AnotherProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	firstID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_first", "First project", "custom-first", ""))
	secondID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_second", "Second project", "custom-second", ""))
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+firstID), authz.ScopeLogRead, "project-1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+secondID), authz.ScopeLogRead, "project-2")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember("", "membership_1", "user_1", "custom-first"))

	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", mock.Anything).Return(&thirdpartyworkos.Member{
		ID:             "membership_1",
		UserID:         "user_1",
		OrganizationID: mockidp.MockOrgID,
		RoleSlugs:      []string{"custom-first", "custom-second"},
		CreatedAt:      mockMembershipTimestamp,
	}, nil).Maybe()

	// The member reads one project's tool logs and is given a second
	// project's: the scope is not new to them, but the project is.
	member, err := ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{firstID, secondID}})
	require.NoError(t, err)
	require.NotNil(t, member)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, "local_user_1", record.SubjectID)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{string(authz.ScopeLogRead)}, metadata["sensitive_scopes_added"])

	// Giving up one of the two projects adds nothing.
	member, err = ti.service.UpdateMemberRoles(ctx, &gen.UpdateMemberRolesPayload{UserID: "local_user_1", RoleIds: []string{secondID}})
	require.NoError(t, err)
	require.NotNil(t, member)
	record, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Nil(t, record.Metadata)
}

func TestRoleManager_AddMemberRole_AuditLog_SensitiveScopesAdded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	orgID := authCtx.ActiveOrganizationID

	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "custom-builder", ""))
	reviewerID := seedRole(t, ctx, ti.conn, orgID, mockRole("role_reviewer", "Reviewer", "custom-reviewer", ""))
	seedGrant(t, ctx, ti.conn, orgID, urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+reviewerID), authz.ScopeLogRead, "project-1")
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_user_1", "ada@example.com", "Ada Lovelace", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, orgID, "local_user_1", mockMember("", "membership_1", "user_1", "custom-builder"))
	actor := RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, "test_actor"), DisplayName: nil}

	// Approving an access request gives the member the role, and the row
	// names what they gain by it.
	tx := testenv.BeginTx(t, ctx, ti.conn)
	result, _, err := ti.service.roleMgr.AddMemberRoleTx(ctx, tx, orgID, "local_user_1", reviewerID, actor, nil)
	require.NoError(t, err)
	require.True(t, result.Changed)
	require.NoError(t, tx.Commit(ctx))

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, "local_user_1", record.SubjectID)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, []any{string(authz.ScopeLogRead)}, metadata["sensitive_scopes_added"])

	// Approving again changes nothing and writes no row.
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	tx = testenv.BeginTx(t, ctx, ti.conn)
	result, _, err = ti.service.roleMgr.AddMemberRoleTx(ctx, tx, orgID, "local_user_1", reviewerID, actor, nil)
	require.NoError(t, err)
	require.False(t, result.Changed)
	require.NoError(t, tx.Commit(ctx))
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, before, after)
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
