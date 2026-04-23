package projects_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestProjectsService_CreateProject_CreatesAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, true)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ctx = withAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Resource: authCtx.ActiveOrganizationID})

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectCreate)
	require.NoError(t, err)

	name := "audit-create-project-" + uuid.NewString()[:8]
	result, err := ti.service.CreateProject(ctx, &gen.CreateProjectPayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           name,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Project)
	require.Equal(t, name, result.Project.Name)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestProjectsService_CreateProject_ForbiddenDoesNotCreateAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, true)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateProject(ctx, &gen.CreateProjectPayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		OrganizationID: "org_not_allowed",
		Name:           "forbidden-project",
	})
	require.Error(t, err)
	require.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestProjectsService_CreateProject_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, true)
	ctx = authz.GrantsToContext(ctx, nil)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	result, err := ti.service.CreateProject(ctx, &gen.CreateProjectPayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "forbidden-without-org-admin",
	})
	require.Error(t, err)
	require.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestProjectsService_CreateProject_SkipsRBACWhenDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, false)
	ctx = authz.GrantsToContext(ctx, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	result, err := ti.service.CreateProject(ctx, &gen.CreateProjectPayload{
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "rbac-disabled-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Project)
}

func TestProjectsService_CreateProject_AuditLogRecord(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, true)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ctx = withAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Resource: authCtx.ActiveOrganizationID})

	name := "audit-create-project-record-" + uuid.NewString()[:8]
	result, err := ti.service.CreateProject(ctx, &gen.CreateProjectPayload{
		ApikeyToken:    nil,
		SessionToken:   nil,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           name,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Project)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionProjectCreate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionProjectCreate), record.Action)
	require.Equal(t, "project", record.SubjectType)
	require.Equal(t, result.Project.Name, record.SubjectDisplay)
	require.Equal(t, string(result.Project.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)
	require.Nil(t, record.Metadata)
}
