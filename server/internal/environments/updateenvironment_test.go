package environments_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestEnvironmentsService_UpdateEnvironment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	t.Run("update environment name and description", func(t *testing.T) {
		t.Parallel()

		// Create initial environment
		env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "initial-env",
			Description:      conv.Ptr("Initial description"),
			Entries: []*gen.EnvironmentEntryInput{
				{Name: "KEY1", Value: "value1"},
			},
		})
		require.NoError(t, err)

		// Update environment
		updatedEnv, err := ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             env.Slug,
			Description:      conv.Ptr("Updated description"),
			Name:             conv.Ptr("updated-env"),
			EntriesToUpdate:  []*gen.EnvironmentEntryInput{},
			EntriesToRemove:  []string{},
		})
		require.NoError(t, err)
		require.NotNil(t, updatedEnv)

		require.Equal(t, env.ID, updatedEnv.ID)
		require.Equal(t, "updated-env", updatedEnv.Name)
		require.Equal(t, "Updated description", *updatedEnv.Description)
		require.Len(t, updatedEnv.Entries, 1)
	})

	t.Run("update environment entries", func(t *testing.T) {
		t.Parallel()

		// Create initial environment
		env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "test-env",
			Description:      nil,
			Entries: []*gen.EnvironmentEntryInput{
				{Name: "KEY1", Value: "value1"},
				{Name: "KEY2", Value: "value2"},
			},
		})
		require.NoError(t, err)

		// Update environment entries
		updatedEnv, err := ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             env.Slug,
			Description:      nil,
			Name:             nil,
			EntriesToUpdate: []*gen.EnvironmentEntryInput{
				{Name: "KEY1", Value: "updated-value1"},
				{Name: "KEY3", Value: "new-value3"},
			},
			EntriesToRemove: []string{"KEY2"},
		})
		require.NoError(t, err)
		require.NotNil(t, updatedEnv)

		require.Len(t, updatedEnv.Entries, 2)

		// Check that entries are properly updated
		entryMap := make(map[string]string)
		for _, entry := range updatedEnv.Entries {
			entryMap[entry.Name] = entry.Value
		}

		require.Contains(t, entryMap, "KEY1")
		require.Contains(t, entryMap, "KEY3")
		require.NotContains(t, entryMap, "KEY2")
	})

	t.Run("update non-existent environment", func(t *testing.T) {
		t.Parallel()

		_, err := ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             "non-existent",
			Description:      nil,
			Name:             nil,
			EntriesToUpdate:  []*gen.EnvironmentEntryInput{},
			EntriesToRemove:  []string{},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "environment not found")
	})
}
