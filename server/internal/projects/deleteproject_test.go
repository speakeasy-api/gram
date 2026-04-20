package projects_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestProjectsService_DeleteProject_CreatesAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, true)
	project := createProjectForDeletion(t, ctx, ti, "audit-delete-project-"+uuid.NewString()[:8])
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Resource: authCtx.ActiveOrganizationID})

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectDelete)
	require.NoError(t, err)

	err = ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{
		ID:           project.ID.String(),
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionProjectDelete)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionProjectDelete), record.Action)
	require.Equal(t, "project", record.SubjectType)
	require.Equal(t, project.Name, record.SubjectDisplay)
	require.Equal(t, project.Slug, record.SubjectSlug)
	require.Nil(t, record.Metadata)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestProjectsService_DeleteProject_InvalidIDDoesNotCreateAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, true)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectDelete)
	require.NoError(t, err)

	err = ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{
		ID:           "not-a-valid-uuid",
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.Error(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestProjectsService_DeleteProject_ForbiddenWithoutOrgAdminGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, true)
	project := createProjectForDeletion(t, ctx, ti, "no-wildcard-"+uuid.NewString()[:8])
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeBuildWrite, Resource: project.ID.String()})

	err := ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{
		ID:           project.ID.String(),
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestProjectsService_DeleteProject_SkipsRBACWhenDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, false)
	project := createProjectForDeletion(t, ctx, ti, "rbac-disabled-"+uuid.NewString()[:8])
	ctx = access.GrantsToContext(ctx, nil)

	err := ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{
		ID: project.ID.String(),
	})
	require.NoError(t, err)
}

func createProjectForDeletion(t *testing.T, ctx context.Context, ti *testInstance, name string) projectsrepo.Project {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           name,
		Slug:           name,
	})
	require.NoError(t, err)

	return project
}

func TestProjectsService_DeleteProject(t *testing.T) {
	t.Parallel()

	t.Run("it rejects deleting with invalid project ID", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestProjectsService(t, true)

		// Try to delete with invalid UUID
		err := ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{
			ID: "not-a-valid-uuid",
		})

		// Should return an invalid error
		require.Error(t, err)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	})

	t.Run("it rejects deleting without auth context", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestProjectsService(t, true)

		// Try to delete without auth context
		err := ti.service.DeleteProject(context.Background(), &gen.DeleteProjectPayload{
			ID: "00000000-0000-0000-0000-000000000001",
		})

		// Should return unauthorized
		require.Error(t, err)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("it rejects deleting a non-existent project", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestProjectsService(t, true)
		nonExistentProjectID := uuid.New()
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		ctx = withAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Resource: authCtx.ActiveOrganizationID})

		err := ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{
			ID: nonExistentProjectID.String(),
		})

		require.Error(t, err)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	})

	t.Run("it rejects deleting a default project", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestProjectsService(t, true)

		// Try to delete a default project, which should be forbidden
		err := ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{
			ID: "00000000-0000-0000-0000-000000000000", // Assuming 0000-0000-0000-0000-000000000000 is the ID for the default project
		})

		// Should return a forbidden error
		require.Error(t, err)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	})
}

func TestProjectsService_DeleteProject_AlreadyDeletedDoesNotCreateAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, true)
	project := createProjectForDeletion(t, ctx, ti, "double-delete-"+uuid.NewString()[:8])

	// First delete should succeed
	err := ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{
		ID: project.ID.String(),
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectDelete)
	require.NoError(t, err)

	// Second delete should not return a misleading error; the soft-deleted
	// project is invisible to the access check so we expect CodeForbidden.
	err = ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{
		ID: project.ID.String(),
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	// No additional audit log should be created
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}
