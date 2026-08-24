package jsonwebkeysets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRevokeKey_Active(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)
	keys := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, keys, 1)

	revokesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyRevoke)
	require.NoError(t, err)

	revoked, err := ti.service.RevokeKey(adminCtx(t, ctx), &gen.RevokeKeyPayload{
		ID:           keys[0].ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "revoked", revoked.KeyState)
	require.NotNil(t, revoked.RevokedAt)

	require.Empty(t, listKeys(t, ctx, ti, set.ID, false))

	revokesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyRevoke)
	require.NoError(t, err)
	require.Equal(t, revokesBefore+1, revokesAfter)
}

func TestRevokeKey_Pending(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	pending, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	revoked, err := ti.service.RevokeKey(adminCtx(t, ctx), &gen.RevokeKeyPayload{
		ID:           pending.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "revoked", revoked.KeyState)

	// The active key is untouched.
	remaining := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, remaining, 1)
	require.Equal(t, "active", remaining[0].KeyState)
}

// TestRevokeKey_RevokedIsNotFound: revocation soft-deletes the row, so a
// second revoke sees no key at all.
func TestRevokeKey_RevokedIsNotFound(t *testing.T) {
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

	_, err = ti.service.RevokeKey(adminCtx(t, ctx), &gen.RevokeKeyPayload{
		ID:           keys[0].ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
