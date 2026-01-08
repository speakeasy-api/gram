package projects_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestProjectsService_GetProject(t *testing.T) {
	t.Parallel()

	t.Run("it returns project successfully", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestProjectsService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectSlug)

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

	t.Run("it rejects without auth context", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestProjectsService(t)

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

		_, ti := newTestProjectsService(t)

		// Create auth context without organization ID
		ctx, err := ti.sessionManager.Authenticate(context.Background(), "", true)
		require.NoError(t, err)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)

		// Clear the organization ID
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

		ctx, ti := newTestProjectsService(t)

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

		ctx, ti := newTestProjectsService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectSlug)

		// Store the project slug from the first context
		projectSlug := *authCtx.ProjectSlug

		// Create a new auth context with a different organization
		ctx2, err := ti.sessionManager.Authenticate(context.Background(), "", true)
		require.NoError(t, err)

		authCtx2, ok := contextvalues.GetAuthContext(ctx2)
		require.True(t, ok)

		// Change the organization ID to simulate a different organization
		authCtx2.ActiveOrganizationID = "different-org-id"

		// Try to get the project from the first context using the second context
		result, err := ti.service.GetProject(ctx2, &gen.GetProjectPayload{
			Slug: types.Slug(projectSlug),
		})

		require.Error(t, err)
		assert.Nil(t, result)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		assert.Equal(t, oops.CodeNotFound, oopsErr.Code)
	})
}
