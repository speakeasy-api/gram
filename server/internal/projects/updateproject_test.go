package projects_test

import (
	"strings"
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
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

	lockTx := testenv.BeginTx(t, ctx, ti.conn)

	projectRepo := repo.New(lockTx)
	_, err := projectRepo.GetProjectByIDForUpdate(ctx, *authCtx.ProjectID)
	require.NoError(t, err)

	renameErr := make(chan error, 1)
	go func() {
		_, err := ti.service.UpdateProject(ctx, &gen.UpdateProjectPayload{Name: "Concurrent rename"})
		renameErr <- err
	}()

	testenv.WaitForBlockedBackend(t, ctx, ti.conn)

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

func TestUpdateProjectRejectsNullName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	before, err := repo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	beforeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)

	result, err := ti.service.UpdateProject(ctx, &gen.UpdateProjectPayload{Name: "invalid\x00name"})
	require.Error(t, err)
	require.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)

	after, err := repo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Equal(t, before.Name, after.Name)
	afterAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAuditCount, afterAuditCount)
}

func TestUpdateProjectRequiresAuthenticationBeforeEmptyNameValidation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	ctx = contextvalues.SetAuthContext(ctx, nil)

	result, err := ti.service.UpdateProject(ctx, &gen.UpdateProjectPayload{Name: ""})
	require.Error(t, err)
	require.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestUpdateProjectAcceptsMaxLengthNameAfterTrimming(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	name := strings.Repeat("a", 40)

	result, err := ti.service.UpdateProject(ctx, &gen.UpdateProjectPayload{
		Name: "  " + name + "  ",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Project)
	require.Equal(t, name, result.Project.Name)
}

func TestUpdateProjectRejectsTooLongName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t)
	result, err := ti.service.UpdateProject(ctx, &gen.UpdateProjectPayload{
		Name: strings.Repeat("a", 41),
	})
	require.Error(t, err)
	require.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
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
