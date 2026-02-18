package environments_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
)

func TestEnvironmentsService_ListEnvironments(t *testing.T) {
	t.Parallel()

	t.Run("list environments when none exist", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)

		result, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Empty(t, result.Environments)
	})

	t.Run("list environments after creating some", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)

		// Create first environment
		env1, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "env-1",
			Description:      new("First environment"),
			Entries: []*gen.EnvironmentEntryInput{
				{Name: "KEY1", Value: "value1"},
			},
		})
		require.NoError(t, err)

		// Create second environment
		env2, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "env-2",
			Description:      nil,
			Entries:          []*gen.EnvironmentEntryInput{},
		})
		require.NoError(t, err)

		// List environments
		result, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Environments, 2)

		// Check that both environments are returned
		envIDs := []string{result.Environments[0].ID, result.Environments[1].ID}
		require.Contains(t, envIDs, env1.ID)
		require.Contains(t, envIDs, env2.ID)

		// Check environment with entries
		for _, env := range result.Environments {
			if env.ID == env1.ID {
				require.Equal(t, "env-1", env.Name)
				require.Equal(t, "First environment", *env.Description)
				require.Len(t, env.Entries, 1)
				require.Equal(t, "KEY1", env.Entries[0].Name)
				require.Contains(t, env.Entries[0].Value, "*") // Should be redacted
			}
			if env.ID == env2.ID {
				require.Equal(t, "env-2", env.Name)
				require.Nil(t, env.Description)
				require.Empty(t, env.Entries)
			}
		}
	})
}
