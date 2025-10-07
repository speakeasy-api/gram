package keys_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestKeysService_ValidateKey(t *testing.T) {
	t.Parallel()

	t.Run("successful validation returns organization and projects", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestKeysService(t)

		result, err := ti.service.ValidateKey(ctx, &gen.ValidateKeyPayload{})
		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify organization
		require.NotNil(t, result.Organization)
		require.NotEmpty(t, result.Organization.ID)
		require.NotEmpty(t, result.Organization.Name)
		require.NotEmpty(t, result.Organization.Slug)

		// Verify projects
		require.NotNil(t, result.Projects)
		require.NotEmpty(t, result.Projects)
		require.Len(t, result.Projects, 1) // InitAuthContext creates one project

		// Verify project fields
		project := result.Projects[0]
		require.NotEmpty(t, project.ID)
		require.NotEmpty(t, project.Name)
		require.NotEmpty(t, project.Slug)
	})

	t.Run("returns multiple projects when they exist", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestKeysService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		// Create additional projects
		projectsRepo := projectsrepo.New(ti.conn)

		project2, err := projectsRepo.CreateProject(ctx, projectsrepo.CreateProjectParams{
			Name:           "test-project-2",
			Slug:           "test-project-2",
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		require.NoError(t, err)

		project3, err := projectsRepo.CreateProject(ctx, projectsrepo.CreateProjectParams{
			Name:           "test-project-3",
			Slug:           "test-project-3",
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		require.NoError(t, err)

		result, err := ti.service.ValidateKey(ctx, &gen.ValidateKeyPayload{})
		require.NoError(t, err)
		require.NotNil(t, result)

		// Should have 3 projects now (1 from InitAuthContext + 2 created above)
		require.Len(t, result.Projects, 3)

		// Verify all projects are returned
		projectIDs := make([]string, len(result.Projects))
		for i, p := range result.Projects {
			projectIDs[i] = p.ID
		}
		require.Contains(t, projectIDs, project2.ID.String())
		require.Contains(t, projectIDs, project3.ID.String())
	})

	t.Run("unauthorized without auth context", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestKeysService(t)

		// Create a context without auth
		ctxWithoutAuth := t.Context()

		_, err := ti.service.ValidateKey(ctxWithoutAuth, &gen.ValidateKeyPayload{})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("returns organization metadata correctly", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestKeysService(t)

		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		result, err := ti.service.ValidateKey(ctx, &gen.ValidateKeyPayload{})
		require.NoError(t, err)

		// Verify organization matches auth context
		require.Equal(t, authCtx.ActiveOrganizationID, result.Organization.ID)
		require.Equal(t, authCtx.OrganizationSlug, result.Organization.Slug)
	})
}
