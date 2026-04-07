package access

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	trequire "github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_DeleteRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	trequire.NotNil(t, authCtx)

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"),
	}, nil).Once()
	ti.roles.On("DeleteRole", mock.Anything, "org_workos_test", "custom-builder").Return(nil).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeBuildRead, "project-1")
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeMCPConnect, WildcardResource)

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	trequire.NoError(t, err)

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"))
	trequire.Empty(t, grants)
}

func TestService_DeleteRole_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{}, nil).Once()

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_missing"})
	trequire.Error(t, err)
	trequire.Contains(t, err.Error(), "role not found")
}

func TestService_DeleteRole_SystemRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockSystemRole("role_admin", "Admin", "admin"),
	}, nil).Once()

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_admin"})
	trequire.Error(t, err)
	trequire.Contains(t, err.Error(), "system roles cannot be deleted")
}

func TestService_DeleteRole_WorkOSDeleteFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	trequire.NotNil(t, authCtx)
	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Custom Builder", "custom-builder", "Old description"),
	}, nil).Once()
	ti.roles.On("DeleteRole", mock.Anything, "org_workos_test", "custom-builder").Return(errors.New("workos unavailable")).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeBuildRead, "project-1")

	err := ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	trequire.Error(t, err)
	trequire.Contains(t, err.Error(), "delete role in workos")

	grants := listPrincipalGrants(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"))
	trequire.Empty(t, grants)

}

func TestService_DeleteRole_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	trequire.True(t, ok)
	trequire.NotNil(t, authCtx)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleDelete)
	trequire.NoError(t, err)

	ti.roles.On("ListRoles", mock.Anything, "org_workos_test").Return([]thirdpartyworkos.Role{
		mockRole("role_custom", "Audit Builder", "custom-builder", "Old description"),
	}, nil).Once()
	ti.roles.On("DeleteRole", mock.Anything, "org_workos_test", "custom-builder").Return(nil).Once()
	seedGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "custom-builder"), ScopeBuildRead, "project-1")

	err = ti.service.DeleteRole(ctx, &gen.DeleteRolePayload{ID: "role_custom"})
	trequire.NoError(t, err)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessRoleDelete)
	trequire.NoError(t, err)
	trequire.Equal(t, string(audit.ActionAccessRoleDelete), record.Action)
	trequire.Equal(t, "access_role", record.SubjectType)
	trequire.Equal(t, "Audit Builder", record.SubjectDisplay)
	trequire.Equal(t, "custom-builder", record.SubjectSlug)
	trequire.Nil(t, record.BeforeSnapshot)
	trequire.Nil(t, record.AfterSnapshot)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessRoleDelete)
	trequire.NoError(t, err)
	trequire.Equal(t, beforeCount+1, afterCount)
}
