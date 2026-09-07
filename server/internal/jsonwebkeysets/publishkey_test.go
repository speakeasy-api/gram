package jsonwebkeysets_test

import (
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestPublishKey_PendingWhenActiveExists publishes into a set that already has
// an active key: the new key enters as pending so verifier caches can warm up
// before it signs anything.
func TestPublishKey_PendingWhenActiveExists(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	publishesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyPublish)
	require.NoError(t, err)

	published, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "pending", published.KeyState)
	require.Nil(t, published.ActivatedAt)
	require.Equal(t, ek.ID, published.ExternalKeyID)

	publishesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeyPublish)
	require.NoError(t, err)
	require.Equal(t, publishesBefore+1, publishesAfter)
}

// TestPublishKey_ActiveWhenNoActive publishes into a set whose only key was
// revoked: with nothing to overlap with, the new key mints straight to active.
func TestPublishKey_ActiveWhenNoActive(t *testing.T) {
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

	published, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "active", published.KeyState)
	require.NotNil(t, published.ActivatedAt)
}

// TestPublishKey_SameKidConflict pins the KMS stub to one shared key, as
// publishing the same un-rotated backing key does in production: the second
// publish derives the same thumbprint kid and must refuse.
func TestPublishKey_SameKidConflict(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ti.kms.UseSharedClient(t, jose.ES256)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	_, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        set.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

// TestPublishKey_RevokedKidStaysBurned revokes the set's key and publishes the
// same backing key again: the kid check deliberately sees soft-deleted rows,
// so a revoked kid can never re-enter the set even though the unique index
// would allow it.
func TestPublishKey_RevokedKidStaysBurned(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ti.kms.UseSharedClient(t, jose.ES256)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)
	keys := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, keys, 1)

	_, err := ti.service.RevokeKey(adminCtx(t, ctx), &gen.RevokeKeyPayload{
		ID:           keys[0].ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        set.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestPublishKey_SetNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestPublishKey_RateLimited drains the per-organization mint budget (shared
// with createSet) and asserts the next publish is refused rather than reaching
// KMS. The per-test organization id keeps the bucket isolated from parallel
// tests.
func TestPublishKey_RateLimited(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	// createSet spent one mint; nine more exhaust the burst of ten.
	for range 9 {
		_, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
			SetID:        set.ID,
			SessionToken: nil,
		})
		require.NoError(t, err)
	}

	_, err := ti.service.PublishKey(adminCtx(t, ctx), &gen.PublishKeyPayload{
		SetID:        set.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeRateLimitExceeded)
}
