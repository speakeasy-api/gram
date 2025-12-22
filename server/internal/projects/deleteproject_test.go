package projects_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestProjectsService_DeleteProject(t *testing.T) {
	t.Parallel()

	t.Run("it rejects deleting with invalid project ID", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestProjectsService(t)

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

		_, ti := newTestProjectsService(t)

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

		ctx, ti := newTestProjectsService(t)

		// Try to delete a valid UUID that doesn't exist
		err := ti.service.DeleteProject(ctx, &gen.DeleteProjectPayload{
			ID: "00000000-0000-0000-0000-000000000001",
		})

		// Should return a forbidden error (project access check fails)
		require.Error(t, err)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	})
}
