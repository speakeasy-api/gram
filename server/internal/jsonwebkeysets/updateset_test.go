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

func TestUpdateSet_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek1 := createBackedGcpKmsKey(t, ctx, ti, "signing-key-v1")
	ek2 := createBackedGcpKmsKey(t, ctx, ti, "signing-key-v2")
	set := createSet(t, ctx, ti, "primary", ek1.ID)

	updatesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeySetUpdate)
	require.NoError(t, err)

	updated, err := ti.service.UpdateSet(adminCtx(t, ctx), &gen.UpdateSetPayload{
		ID:            set.ID,
		SessionToken:  nil,
		Name:          "rotated",
		ExternalKeyID: ek2.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "rotated", updated.Name)
	require.Equal(t, ek2.ID, updated.ExternalKeyID)

	// Re-pointing the set does not touch the already-published key: it stays
	// pinned to the external key it was minted from.
	keys := listKeys(t, ctx, ti, set.ID, false)
	require.Len(t, keys, 1)
	require.Equal(t, ek1.ID, keys[0].ExternalKeyID)

	updatesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionJsonWebKeySetUpdate)
	require.NoError(t, err)
	require.Equal(t, updatesBefore+1, updatesAfter)

	// The update event's snapshots evidence the re-point, not just that an
	// update happened.
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionJsonWebKeySetUpdate)
	require.NoError(t, err)
	require.Equal(t, set.ID, record.SubjectID)

	before, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "primary", before["name"])
	require.Equal(t, ek1.ID, before["external_key_id"])

	after, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "rotated", after["name"])
	require.Equal(t, ek2.ID, after["external_key_id"])
}

func TestUpdateSet_AwsRepointRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	aws := createAwsKmsKey(t, ctx, ti, "aws-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	_, err := ti.service.UpdateSet(adminCtx(t, ctx), &gen.UpdateSetPayload{
		ID:            set.ID,
		SessionToken:  nil,
		Name:          "primary",
		ExternalKeyID: aws.ID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// The refusal left the set untouched.
	got, err := ti.service.GetSet(readCtx(t, ctx), &gen.GetSetPayload{
		ID:           set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, ek.ID, got.ExternalKeyID)
}

func TestUpdateSet_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")

	_, err := ti.service.UpdateSet(adminCtx(t, ctx), &gen.UpdateSetPayload{
		ID:            uuid.NewString(),
		SessionToken:  nil,
		Name:          "primary",
		ExternalKeyID: ek.ID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateSet_NameRequired(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	_, err := ti.service.UpdateSet(adminCtx(t, ctx), &gen.UpdateSetPayload{
		ID:            set.ID,
		SessionToken:  nil,
		Name:          "  ",
		ExternalKeyID: ek.ID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
