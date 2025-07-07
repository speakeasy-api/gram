package environments_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
)

func TestEnvironmentsService_DeleteEnvironment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	t.Run("delete existing environment", func(t *testing.T) {
		t.Parallel()

		// Create environment
		env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "to-delete",
			Description:      nil,
			Entries: []*gen.EnvironmentEntryInput{
				{Name: "KEY1", Value: "value1"},
			},
		})
		require.NoError(t, err)

		// Delete environment
		err = ti.service.DeleteEnvironment(ctx, &gen.DeleteEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             env.Slug,
		})
		require.NoError(t, err)

		// Verify environment is deleted by trying to list environments
		result, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		require.Empty(t, result.Environments)
	})

	t.Run("delete non-existent environment", func(t *testing.T) {
		t.Parallel()

		// Try to delete non-existent environment
		err := ti.service.DeleteEnvironment(ctx, &gen.DeleteEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             "non-existent",
		})
		// This should not error based on the implementation
		require.NoError(t, err)
	})
}
