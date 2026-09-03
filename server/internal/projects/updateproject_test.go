package projects_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestUpdateProjectRenamesDisplayName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	before, err := repo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	beforeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)

	result, err := ti.service.UpdateProject(ctx, &gen.UpdateProjectPayload{
		Name: "  Renamed project  ",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Project)
	require.Equal(t, "Renamed project", result.Project.Name)
	require.Equal(t, before.Slug, string(result.Project.Slug))

	after, err := repo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Equal(t, "Renamed project", after.Name)
	require.Equal(t, before.Slug, after.Slug)

	afterAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAuditCount+1, afterAuditCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, before.Name, beforeSnapshot["Name"])
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Renamed project", afterSnapshot["Name"])
}

func TestUpdateProjectWaitsForConcurrentDeleteAndReturnsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	lockTx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = lockTx.Rollback(ctx) }()

	projectRepo := repo.New(lockTx)
	_, err = projectRepo.GetProjectByIDForUpdate(ctx, *authCtx.ProjectID)
	require.NoError(t, err)

	renameErr := make(chan error, 1)
	go func() {
		_, err := ti.service.UpdateProject(ctx, &gen.UpdateProjectPayload{Name: "Concurrent rename"})
		renameErr <- err
	}()

	require.Eventually(t, func() bool {
		var waiting bool
		err := ti.conn.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM pg_stat_activity
  WHERE datname = current_database()
    AND wait_event_type = 'Lock'
    AND query LIKE '%GetProjectByIDForUpdate%'
)`).Scan(&waiting)
		return err == nil && waiting
	}, 2*time.Second, 10*time.Millisecond, "rename did not wait while locking the project snapshot")

	_, err = projectRepo.DeleteProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.NoError(t, lockTx.Commit(ctx))

	select {
	case err = <-renameErr:
	case <-time.After(2 * time.Second):
		t.Fatal("rename did not finish after the delete committed")
	}
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestUpdateProjectRejectsBlankName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	beforeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)

	result, err := ti.service.UpdateProject(ctx, &gen.UpdateProjectPayload{
		Name: "   ",
	})
	require.Error(t, err)
	require.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, oops.CodeInvalid, oopsErr.Code)

	afterAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAuditCount, afterAuditCount)
}

func TestUpdateProjectRequiresProjectWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	ctx = authz.GrantsToContext(ctx, nil)

	result, err := ti.service.UpdateProject(ctx, &gen.UpdateProjectPayload{
		Name: "Renamed project",
	})
	require.Error(t, err)
	require.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
