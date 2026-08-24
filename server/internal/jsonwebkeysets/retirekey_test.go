package jsonwebkeysets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestRetireKey_Success is the graceful decommission: the active key leaves
// signing use without a successor, the schema's zero-active state.
func TestRetireKey_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)
	keys := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, keys, 1)

	retiresBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyRetire)
	require.NoError(t, err)

	retired, err := ti.service.RetireKey(adminCtx(t, ctx), &gen.RetireKeyPayload{
		ID:           keys[0].ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "retired", retired.KeyState)
	require.NotNil(t, retired.RetiredAt)

	// The key stays published: retirement preserves verification of tokens
	// already signed with it.
	remaining := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, remaining, 1)
	require.Equal(t, "retired", remaining[0].KeyState)

	retiresAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyRetire)
	require.NoError(t, err)
	require.Equal(t, retiresBefore+1, retiresAfter)
}

func TestRetireKey_PendingConflict(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	pending, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.RetireKey(adminCtx(t, ctx), &gen.RetireKeyPayload{
		ID:           pending.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestRetireKey_RetiredIsNoOp(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)
	keys := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, keys, 1)

	_, err := ti.service.RetireKey(adminCtx(t, ctx), &gen.RetireKeyPayload{
		ID:           keys[0].ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	retiresBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyRetire)
	require.NoError(t, err)

	again, err := ti.service.RetireKey(adminCtx(t, ctx), &gen.RetireKeyPayload{
		ID:           keys[0].ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "retired", again.KeyState)

	retiresAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyRetire)
	require.NoError(t, err)
	require.Equal(t, retiresBefore, retiresAfter)
}
