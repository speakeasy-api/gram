package keys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestKeysService_RevokeKey(t *testing.T) {
	t.Parallel()

	t.Run("successful key revocation", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)

		// Create a key first
		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "key-to-revoke",
			Scopes:       []string{"consumer"},
		})
		require.NoError(t, err)

		// Revoke the key
		err = ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{
			ID:           key.ID,
			SessionToken: nil,
		})
		require.NoError(t, err)

		// Verify the key is no longer in the list
		result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{
			SessionToken: nil,
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		for _, listedKey := range result.Keys {
			require.NotEqual(t, key.ID, listedKey.ID, "revoked key should not appear in list")
		}
	})

	t.Run("revoke non-existent key", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)

		// Try to revoke a non-existent key
		nonExistentID := uuid.New().String()
		err := ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{
			ID:           nonExistentID,
			SessionToken: nil,
		})

		// This should not error, as the delete operation would be idempotent
		// The SQL query would just not match any rows
		require.NoError(t, err)
	})

	t.Run("invalid key ID format", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)

		// Try to revoke with an invalid UUID format
		err := ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{
			ID:           "invalid-uuid",
			SessionToken: nil,
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
		require.Contains(t, err.Error(), "invalid key ID format")
	})

	t.Run("unauthorized without auth context", func(t *testing.T) {
		t.Parallel()
		_, ti := newTestKeysService(t)

		// Create a context without auth
		ctxWithoutAuth := t.Context()

		// Try to revoke a key
		err := ti.service.RevokeKey(ctxWithoutAuth, &gen.RevokeKeyPayload{
			ID:           uuid.New().String(),
			SessionToken: nil,
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})

	t.Run("revoke multiple keys", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)

		// Create multiple keys
		key1, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "key-1-to-revoke",
			Scopes:       []string{"consumer"},
		})
		require.NoError(t, err)

		key2, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "key-2-to-revoke",
			Scopes:       []string{"consumer"},
		})
		require.NoError(t, err)

		key3, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			SessionToken: nil,
			Name:         "key-3-to-keep",
			Scopes:       []string{"producer"},
		})
		require.NoError(t, err)

		// Revoke first two keys
		err = ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{
			ID:           key1.ID,
			SessionToken: nil,
		})
		require.NoError(t, err)

		err = ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{
			ID:           key2.ID,
			SessionToken: nil,
		})
		require.NoError(t, err)

		// Verify only the third key remains
		result, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{
			SessionToken: nil,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Keys, 1)
		require.Equal(t, key3.ID, result.Keys[0].ID)
		require.Equal(t, "key-3-to-keep", result.Keys[0].Name)
	})
}
