package jsonwebkeysets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestListKeys_RevokedVisibility revokes a key and asserts it drops out of the
// default listing while include_revoked surfaces it as revocation history.
func TestListKeys_RevokedVisibility(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	published, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	revoked, err := ti.service.RevokeKey(adminCtx(t, ctx), &gen.RevokeKeyPayload{
		ID:           published.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "revoked", revoked.KeyState)
	require.NotNil(t, revoked.RevokedAt)

	visible := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, visible, 1)
	require.NotEqual(t, published.ID, visible[0].ID)

	all := listKeys(t, ctx, ti, set.ID, true)
	require.Len(t, all, 2)

	var foundRevoked bool
	for _, key := range all {
		if key.ID == published.ID {
			foundRevoked = true
			require.Equal(t, "revoked", key.KeyState)
			require.NotNil(t, key.RevokedAt)
		}
	}
	require.True(t, foundRevoked)
}

func TestListKeys_SetNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.ListKeys(readCtx(t, ctx), &gen.ListKeysPayload{
		SetID:          uuid.NewString(),
		IncludeRevoked: false,
		SessionToken:   nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestListKeys_DeletedSetNotFound asserts a deleted set's keys are not
// reachable through the listing: the set lookup 404s before any keys query.
func TestListKeys_DeletedSetNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	require.NoError(t, ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{
		ID:           set.ID,
		SessionToken: nil,
	}))

	_, err := ti.service.ListKeys(readCtx(t, ctx), &gen.ListKeysPayload{
		SetID:          set.ID,
		IncludeRevoked: true,
		SessionToken:   nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
