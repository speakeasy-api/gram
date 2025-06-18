package keys_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/gen/keys"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/oops"
)

func TestKeysService_CreateKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)

	t.Run("successful key creation", func(t *testing.T) {
		t.Parallel()

		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			Name:         "test-api-key",
			SessionToken: nil,
		})
		require.NoError(t, err)

		require.NotEmpty(t, key.ID)
		require.Equal(t, "test-api-key", key.Name)
		require.NotEmpty(t, key.OrganizationID)
		require.NotEmpty(t, key.CreatedByUserID)
		require.NotNil(t, key.Key)
		require.NotEmpty(t, *key.Key)
		require.Greater(t, len(*key.Key), 64) // Should be prefix + token
		require.Contains(t, *key.Key, "gram_local_")
		require.NotEmpty(t, key.KeyPrefix)
		require.Equal(t, []string{"consumer"}, key.Scopes)
		require.NotEmpty(t, key.CreatedAt)
		require.NotEmpty(t, key.UpdatedAt)

		// Verify the key follows expected format
		require.Contains(t, key.KeyPrefix, "gram_local_")
		require.Greater(t, len(key.KeyPrefix), len("gram_local_"))
	})

	t.Run("key creation without project context", func(t *testing.T) {
		t.Parallel()

		// Ensure there's no project ID in the auth context
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		// Explicitly set projectID to nil
		originalProjectID := authCtx.ProjectID
		authCtx.ProjectID = nil
		ctxWithoutProject := contextvalues.SetAuthContext(ctx, authCtx)

		key, err := ti.service.CreateKey(ctxWithoutProject, &gen.CreateKeyPayload{
			Name:         "org-scoped-key",
			SessionToken: nil,
		})
		require.NoError(t, err)

		require.NotEmpty(t, key.ID)
		require.Equal(t, "org-scoped-key", key.Name)
		require.Nil(t, key.ProjectID)

		// Restore original project ID
		authCtx.ProjectID = originalProjectID
	})

	t.Run("unauthorized without auth context", func(t *testing.T) {
		t.Parallel()

		// Create a context without auth
		ctxWithoutAuth := t.Context()

		_, err := ti.service.CreateKey(ctxWithoutAuth, &gen.CreateKeyPayload{
			Name:         "unauthorized-key",
			SessionToken: nil,
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("multiple keys have unique tokens", func(t *testing.T) {
		t.Parallel()

		key1, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			Name:         "key-1",
			SessionToken: nil,
		})
		require.NoError(t, err)

		key2, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			Name:         "key-2",
			SessionToken: nil,
		})
		require.NoError(t, err)

		require.NotEqual(t, key1.ID, key2.ID)
		require.NotEqual(t, *key1.Key, *key2.Key)
		require.NotEqual(t, key1.KeyPrefix, key2.KeyPrefix)
	})
}