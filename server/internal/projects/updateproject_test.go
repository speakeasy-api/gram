package projects_test

import (
	"testing"

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
