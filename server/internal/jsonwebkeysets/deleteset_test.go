package jsonwebkeysets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestDeleteSet_CascadesKeys deletes a set holding two live keys and asserts
// the cascade: the set is gone, and one key-deletion audit event was emitted
// per withdrawn key, before the set's own deletion event.
func TestDeleteSet_CascadesKeys(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	_, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	setDeletesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeySetDelete)
	require.NoError(t, err)
	keyDeletesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{
		ID:           set.ID,
		SessionToken: nil,
	}))

	_, err = ti.service.GetSet(readCtx(t, ctx), &gen.GetSetPayload{
		ID:           set.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	setDeletesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeySetDelete)
	require.NoError(t, err)
	require.Equal(t, setDeletesBefore+1, setDeletesAfter)

	keyDeletesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyDelete)
	require.NoError(t, err)
	require.Equal(t, keyDeletesBefore+2, keyDeletesAfter)
}

func TestDeleteSet_MissingIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	setDeletesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeySetDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	}))

	setDeletesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeySetDelete)
	require.NoError(t, err)
	require.Equal(t, setDeletesBefore, setDeletesAfter)
}

// TestDeleteSet_RevokedKeysNotDoubleDeleted revokes the set's only key first:
// the cascade must not re-delete the already-withdrawn row, so the delete
// emits zero key-deletion events.
func TestDeleteSet_RevokedKeysNotDoubleDeleted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)
	keys := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, keys, 1)

	_, err := ti.service.RevokeKey(adminCtx(t, ctx), &gen.RevokeKeyPayload{
		ID:           keys[0].ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	keyDeletesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{
		ID:           set.ID,
		SessionToken: nil,
	}))

	keyDeletesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyDelete)
	require.NoError(t, err)
	require.Equal(t, keyDeletesBefore, keyDeletesAfter)
}
