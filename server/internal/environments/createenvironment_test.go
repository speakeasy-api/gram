package environments_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
)

func TestEnvironmentsService_CreateEnvironment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	t.Run("create environment with entries", func(t *testing.T) {
		t.Parallel()

		payload := &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "test-env",
			Description:      new("Test environment description"),
			Entries: []*gen.EnvironmentEntryInput{
				{
					Name:  "API_KEY",
					Value: "secret-key-123",
				},
				{
					Name:  "DATABASE_URL",
					Value: "postgres://localhost:5432/testdb",
				},
			},
		}

		env, err := ti.service.CreateEnvironment(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, env)

		require.NotEmpty(t, env.ID)
		require.Equal(t, "test-env", env.Name)
		require.Equal(t, "test-env", string(env.Slug))
		require.Equal(t, "Test environment description", *env.Description)
		require.Len(t, env.Entries, 2)

		// Check that values are redacted
		for _, entry := range env.Entries {
			require.NotEmpty(t, entry.Name)
			require.Contains(t, entry.Value, "*")
			require.NotEmpty(t, entry.CreatedAt)
			require.NotEmpty(t, entry.UpdatedAt)
		}
	})

	t.Run("create environment without entries", func(t *testing.T) {
		t.Parallel()

		payload := &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "empty-env",
			Description:      nil,
			Entries:          []*gen.EnvironmentEntryInput{},
		}

		env, err := ti.service.CreateEnvironment(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, env)

		require.NotEmpty(t, env.ID)
		require.Equal(t, "empty-env", env.Name)
		require.Equal(t, "empty-env", string(env.Slug))
		require.Nil(t, env.Description)
		require.Empty(t, env.Entries)
	})

	t.Run("create environment with slug generation", func(t *testing.T) {
		t.Parallel()

		payload := &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "Test Environment With Spaces",
			Description:      nil,
			Entries:          []*gen.EnvironmentEntryInput{},
		}

		env, err := ti.service.CreateEnvironment(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, env)

		require.Equal(t, "Test Environment With Spaces", env.Name)
		require.Equal(t, "test-environment-with-spaces", string(env.Slug))
	})
}
