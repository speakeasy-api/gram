package keys_test

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/must"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func randstr(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n := must.Value(rand.Int(rand.Reader, big.NewInt(int64(len(charset)))))
		b[i] = charset[int(n.Int64())]
	}
	return "-" + string(b)
}

func TestKeysService_CreateKey(t *testing.T) {
	t.Parallel()

	t.Run("successful key creation with default scope", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)

		name := "test-api-key" + randstr(6)

		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         name,
			Scopes:       []string{},
		})
		require.NoError(t, err)

		require.NotEmpty(t, key.ID)
		require.Equal(t, name, key.Name)
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

	t.Run("successful key creation with custom scope", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)

		name := "test-api-key" + randstr(6)

		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         name,
			Scopes:       []string{"producer"},
		})
		require.NoError(t, err)

		require.NotEmpty(t, key.ID)
		require.Equal(t, name, key.Name)
		require.NotEmpty(t, key.OrganizationID)
		require.NotEmpty(t, key.CreatedByUserID)
		require.NotNil(t, key.Key)
		require.NotEmpty(t, *key.Key)
		require.Greater(t, len(*key.Key), 64) // Should be prefix + token
		require.Contains(t, *key.Key, "gram_local_")
		require.NotEmpty(t, key.KeyPrefix)
		require.Equal(t, []string{"producer"}, key.Scopes)
		require.NotEmpty(t, key.CreatedAt)
		require.NotEmpty(t, key.UpdatedAt)

		// Verify the key follows expected format
		require.Contains(t, key.KeyPrefix, "gram_local_")
		require.Greater(t, len(key.KeyPrefix), len("gram_local_"))
	})

	t.Run("successful key creation with multiple scopes", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)

		name := "test-api-key" + randstr(6)

		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         name,
			Scopes:       []string{"producer", "consumer"},
		})
		require.NoError(t, err)

		require.NotEmpty(t, key.ID)
		require.Equal(t, name, key.Name)
		require.NotEmpty(t, key.OrganizationID)
		require.NotEmpty(t, key.CreatedByUserID)
		require.NotNil(t, key.Key)
		require.NotEmpty(t, *key.Key)
		require.Greater(t, len(*key.Key), 64) // Should be prefix + token
		require.Contains(t, *key.Key, "gram_local_")
		require.NotEmpty(t, key.KeyPrefix)
		require.Equal(t, []string{"consumer", "producer"}, key.Scopes) // will be sorted
		require.NotEmpty(t, key.CreatedAt)
		require.NotEmpty(t, key.UpdatedAt)

		// Verify the key follows expected format
		require.Contains(t, key.KeyPrefix, "gram_local_")
		require.Greater(t, len(key.KeyPrefix), len("gram_local_"))
	})

	t.Run("successful key creation with invalid scope", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)

		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "test-api-key" + randstr(6),
			Scopes:       []string{"nonexistent_scope"},
		})
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
		require.Nil(t, key)

	})

	t.Run("successful key creation with invalid scopes", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)

		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "test-api-key" + randstr(6),
			Scopes:       []string{"nonexistent_scope", "consumer"}, // one valid, one invalid
		})
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
		require.Nil(t, key)

	})

	t.Run("key creation without project context", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)

		// Ensure there's no project ID in the auth context
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx)

		// Explicitly set projectID to nil
		originalProjectID := authCtx.ProjectID
		authCtx.ProjectID = nil
		ctxWithoutProject := contextvalues.SetAuthContext(ctx, authCtx)

		key, err := ti.service.CreateKey(ctxWithoutProject, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "org-scoped-key",
			Scopes:       []string{"consumer"},
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
		_, ti := newTestKeysService(t)

		// Create a context without auth
		ctxWithoutAuth := t.Context()

		_, err := ti.service.CreateKey(ctxWithoutAuth, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "unauthorized-key",
			Scopes:       []string{"consumer"},
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("multiple keys have unique tokens", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)

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

		require.NotEqual(t, key1.ID, key2.ID)
		require.NotEqual(t, *key1.Key, *key2.Key)
		require.NotEqual(t, key1.KeyPrefix, key2.KeyPrefix)
	})
}
