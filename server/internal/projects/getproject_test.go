package projects_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestProjectsService_GetProject(t *testing.T) {
	t.Parallel()

	t.Run("it returns project successfully", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestProjectsService(t, true)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectSlug)
		ctx = withAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))

		result, err := ti.service.GetProject(ctx, &gen.GetProjectPayload{
			Slug: types.Slug(*authCtx.ProjectSlug),
		})

		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Project)

		assert.Equal(t, authCtx.ProjectID.String(), result.Project.ID)
		assert.Equal(t, *authCtx.ProjectSlug, string(result.Project.Slug))
		assert.Equal(t, authCtx.ActiveOrganizationID, result.Project.OrganizationID)
		assert.NotEmpty(t, result.Project.Name)
		assert.NotEmpty(t, result.Project.CreatedAt)
		assert.NotEmpty(t, result.Project.UpdatedAt)
	})

	t.Run("it skips RBAC when feature is disabled", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestProjectsService(t, false)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectSlug)

		result, err := ti.service.GetProject(ctx, &gen.GetProjectPayload{
			Slug: types.Slug(*authCtx.ProjectSlug),
		})

		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.Project)
	})

	t.Run("it returns not found when build read access is missing", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestProjectsService(t, true)
		ctx = authz.GrantsToContext(ctx, nil)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectSlug)

		result, err := ti.service.GetProject(ctx, &gen.GetProjectPayload{
			Slug: types.Slug(*authCtx.ProjectSlug),
		})

		require.Error(t, err)
		assert.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		assert.Equal(t, oops.CodeNotFound, oopsErr.Code)
	})

	t.Run("it rejects without auth context", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestProjectsService(t, true)

		result, err := ti.service.GetProject(context.Background(), &gen.GetProjectPayload{
			Slug: "some-slug",
		})

		require.Error(t, err)
		assert.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		assert.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("it rejects without active organization", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestProjectsService(t, true)

		// Get the existing auth context and clear the organization ID
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		authCtx.ActiveOrganizationID = ""

		result, err := ti.service.GetProject(ctx, &gen.GetProjectPayload{
			Slug: "some-slug",
		})

		require.Error(t, err)
		assert.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		assert.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("it returns not found for non-existent project", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestProjectsService(t, true)

		result, err := ti.service.GetProject(ctx, &gen.GetProjectPayload{
			Slug: "non-existent-project-slug",
		})

		require.Error(t, err)
		assert.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		assert.Equal(t, oops.CodeNotFound, oopsErr.Code)
	})

	t.Run("it returns not found for project in different organization", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestProjectsService(t, true)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectSlug)
		ctx = withAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))

		// Store the project slug from the first context
		projectSlug := *authCtx.ProjectSlug

		// Modify the auth context to simulate a different organization
		authCtx.ActiveOrganizationID = "different-org-id"

		// Try to get the project using a different organization context
		result, err := ti.service.GetProject(ctx, &gen.GetProjectPayload{
			Slug: types.Slug(projectSlug),
		})

		require.Error(t, err)
		assert.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		assert.Equal(t, oops.CodeNotFound, oopsErr.Code)
	})
}
