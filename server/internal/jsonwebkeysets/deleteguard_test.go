package jsonwebkeysets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	extkeysgen "github.com/speakeasy-api/gram/server/gen/external_keys"
	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// These tests own the refusal-path coverage for the externalkeys delete guard:
// deleting an external key must be refused while a live JSON Web Key Set or
// published key references it. They live here rather than in the externalkeys
// package because only this package's service can create the referencing
// fixtures.

// TestExternalKeyDelete_RefusedWhileSetReferences: the set's backing reference
// alone (its published key pins the same external key) holds the guard closed.
func TestExternalKeyDelete_RefusedWhileSetReferences(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	createSet(t, ctx, ti, "primary", ek.ID)

	err := ti.ekService.DeleteGcpKmsKey(adminCtx(t, ctx), &extkeysgen.DeleteGcpKmsKeyPayload{
		ID:           ek.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

// TestExternalKeyDelete_RefusedWhileOnlyKeyReferences re-points the set at a
// second external key, so the first is referenced only by the published key
// row that was minted from it — the keys arm of the guard.
func TestExternalKeyDelete_RefusedWhileOnlyKeyReferences(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek1 := createBackedGcpKmsKey(t, ctx, ti, "signing-key-v1")
	ek2 := createBackedGcpKmsKey(t, ctx, ti, "signing-key-v2")
	set := createSet(t, ctx, ti, "primary", ek1.ID)

	_, err := ti.service.UpdateSet(adminCtx(t, ctx), &gen.UpdateSetPayload{
		ID:            set.ID,
		SessionToken:  nil,
		Name:          "primary",
		ExternalKeyID: ek2.ID,
	})
	require.NoError(t, err)

	// ek1: referenced only by the published key row.
	err = ti.ekService.DeleteGcpKmsKey(adminCtx(t, ctx), &extkeysgen.DeleteGcpKmsKeyPayload{
		ID:           ek1.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	// ek2: referenced only by the set row (nothing published from it yet) — the
	// sets arm is not redundant with the keys arm.
	err = ti.ekService.DeleteGcpKmsKey(adminCtx(t, ctx), &extkeysgen.DeleteGcpKmsKeyPayload{
		ID:           ek2.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

// TestExternalKeyDelete_SucceedsAfterSetDeleted: deleting the set cascades to
// its keys, releasing both arms of the guard.
func TestExternalKeyDelete_SucceedsAfterSetDeleted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	require.NoError(t, ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{
		ID:           set.ID,
		SessionToken: nil,
	}))

	require.NoError(t, ti.ekService.DeleteGcpKmsKey(adminCtx(t, ctx), &extkeysgen.DeleteGcpKmsKeyPayload{
		ID:           ek.ID,
		SessionToken: nil,
	}))
}

// TestExternalKeyDelete_SoftDeletedReferenceDoesNotBlock: after the set is
// re-pointed away and the key published from the old external key is revoked
// (soft-deleted), that external key has only tombstoned references left and
// must delete cleanly.
func TestExternalKeyDelete_SoftDeletedReferenceDoesNotBlock(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek1 := createBackedGcpKmsKey(t, ctx, ti, "signing-key-v1")
	ek2 := createBackedGcpKmsKey(t, ctx, ti, "signing-key-v2")
	set := createSet(t, ctx, ti, "primary", ek1.ID)
	keys := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, keys, 1)

	_, err := ti.service.UpdateSet(adminCtx(t, ctx), &gen.UpdateSetPayload{
		ID:            set.ID,
		SessionToken:  nil,
		Name:          "primary",
		ExternalKeyID: ek2.ID,
	})
	require.NoError(t, err)

	_, err = ti.service.RevokeKey(adminCtx(t, ctx), &gen.RevokeKeyPayload{
		ID:           keys[0].ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.NoError(t, ti.ekService.DeleteGcpKmsKey(adminCtx(t, ctx), &extkeysgen.DeleteGcpKmsKeyPayload{
		ID:           ek1.ID,
		SessionToken: nil,
	}))
}
