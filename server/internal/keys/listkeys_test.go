package keys_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestKeysService_ListKeys(t *testing.T) {
	t.Parallel()
	t.Run("list empty keys", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestKeysService(t)

		result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{
			SessionToken: nil,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Empty(t, result.Keys)
	})

	t.Run("list keys after creating some", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestKeysService(t)

		// Create a few keys
		key1, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "key-1",
			Scopes:       []string{"consumer"},
		})
		require.NoError(t, err)

		key2, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "key-2",
			Scopes:       []string{"consumer"},
		})
		require.NoError(t, err)

		// List keys
		result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{
			SessionToken: nil,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Keys, 2)

		// Check that keys are returned correctly
		keyNames := make([]string, len(result.Keys))
		for i, key := range result.Keys {
			keyNames[i] = key.Name
		}
		require.ElementsMatch(t, []string{"key-1", "key-2"}, keyNames)

		// Verify key fields
		for _, key := range result.Keys {
			require.NotEmpty(t, key.ID)
			require.NotEmpty(t, key.Name)
			require.NotEmpty(t, key.OrganizationID)
			require.NotEmpty(t, key.CreatedByUserID)
			require.Nil(t, key.Key) // Key should not be returned in list
			require.NotEmpty(t, key.KeyPrefix)
			require.Equal(t, []string{"consumer"}, key.Scopes)
			require.NotEmpty(t, key.CreatedAt)
			require.NotEmpty(t, key.UpdatedAt)

			// Verify the key prefix follows expected format
			require.Contains(t, key.KeyPrefix, "gram_local_")
		}

		// Verify the created keys match the listed ones
		for _, createdKey := range []*gen.Key{key1, key2} {
			found := false
			for _, listedKey := range result.Keys {
				if listedKey.ID == createdKey.ID {
					found = true
					require.Equal(t, createdKey.Name, listedKey.Name)
					require.Equal(t, createdKey.OrganizationID, listedKey.OrganizationID)
					require.Equal(t, createdKey.ProjectID, listedKey.ProjectID)
					require.Equal(t, createdKey.CreatedByUserID, listedKey.CreatedByUserID)
					require.Equal(t, createdKey.KeyPrefix, listedKey.KeyPrefix)
					require.Equal(t, createdKey.Scopes, listedKey.Scopes)
					require.Equal(t, createdKey.CreatedAt, listedKey.CreatedAt)
					require.Equal(t, createdKey.UpdatedAt, listedKey.UpdatedAt)
					break
				}
			}
			require.True(t, found, "created key %s not found in list", createdKey.ID)
		}
	})

	t.Run("unauthorized without auth context", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestKeysService(t)

		// Create a context without auth
		ctxWithoutAuth := t.Context()

		_, err := ti.service.ListKeys(ctxWithoutAuth, &gen.ListKeysPayload{
			SessionToken: nil,
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})
}
