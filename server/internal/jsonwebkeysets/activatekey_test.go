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

// TestActivateKey_PromotesPendingAndRetiresActive is the rotation step: the
// pending key becomes active, and the previously active key retires in the
// same operation — each with its own audit event.
func TestActivateKey_PromotesPendingAndRetiresActive(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)
	first := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, first, 1)

	pending, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	activatesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyActivate)
	require.NoError(t, err)
	retiresBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyRetire)
	require.NoError(t, err)

	activated, err := ti.service.ActivateKey(adminCtx(t, ctx), &gen.ActivateKeyPayload{
		ID:           pending.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "active", activated.KeyState)
	require.NotNil(t, activated.ActivatedAt)

	keys := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, keys, 2)
	for _, key := range keys {
		if key.ID == first[0].ID {
			require.Equal(t, "retired", key.KeyState)
			require.NotNil(t, key.RetiredAt)
		}
	}

	activatesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyActivate)
	require.NoError(t, err)
	require.Equal(t, activatesBefore+1, activatesAfter)

	retiresAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyRetire)
	require.NoError(t, err)
	require.Equal(t, retiresBefore+1, retiresAfter)

	// The activate event's snapshots record the transition itself, and its
	// metadata ties the key back to the set.
	activateRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionJsonWebKeyActivate)
	require.NoError(t, err)
	require.Equal(t, pending.ID, activateRecord.SubjectID)
	require.Equal(t, pending.Kid, activateRecord.SubjectDisplay)

	activateBefore, err := audittest.DecodeAuditData(activateRecord.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "pending", activateBefore["state"])
	require.Equal(t, pending.Kid, activateBefore["kid"])

	activateAfter, err := audittest.DecodeAuditData(activateRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "active", activateAfter["state"])

	activateMetadata, err := audittest.DecodeAuditData(activateRecord.Metadata)
	require.NoError(t, err)
	require.Equal(t, set.ID, activateMetadata["json_web_key_set_id"])

	// The implicit retirement of the previous key records its own transition.
	retireRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionJsonWebKeyRetire)
	require.NoError(t, err)
	require.Equal(t, first[0].ID, retireRecord.SubjectID)

	retireBefore, err := audittest.DecodeAuditData(retireRecord.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "active", retireBefore["state"])

	retireAfter, err := audittest.DecodeAuditData(retireRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "retired", retireAfter["state"])
	require.Equal(t, first[0].Kid, retireAfter["kid"])
}

func TestActivateKey_AlreadyActiveIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)
	keys := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, keys, 1)

	activatesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyActivate)
	require.NoError(t, err)

	activated, err := ti.service.ActivateKey(adminCtx(t, ctx), &gen.ActivateKeyPayload{
		ID:           keys[0].ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "active", activated.KeyState)

	activatesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyActivate)
	require.NoError(t, err)
	require.Equal(t, activatesBefore, activatesAfter)
}

// TestActivateKey_ReactivatesRetired rolls a rotation back: after promoting
// the new key, activating the old (now retired) key must swap the two again.
func TestActivateKey_ReactivatesRetired(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)
	first := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, first, 1)

	pending, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.ActivateKey(adminCtx(t, ctx), &gen.ActivateKeyPayload{
		ID:           pending.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	rolledBack, err := ti.service.ActivateKey(adminCtx(t, ctx), &gen.ActivateKeyPayload{
		ID:           first[0].ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "active", rolledBack.KeyState)
	require.Nil(t, rolledBack.RetiredAt)

	keys := listKeys(t, ctx, ti, set.ID, false)
	for _, key := range keys {
		if key.ID == pending.ID {
			require.Equal(t, "retired", key.KeyState)
		}
	}
}

func TestActivateKey_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.ActivateKey(adminCtx(t, ctx), &gen.ActivateKeyPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestActivateKey_RevokedIsNotFound: a revoked key is withdrawn (soft-deleted)
// entirely, so it cannot be brought back through activation.
func TestActivateKey_RevokedIsNotFound(t *testing.T) {
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

	_, err = ti.service.ActivateKey(adminCtx(t, ctx), &gen.ActivateKeyPayload{
		ID:           keys[0].ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
