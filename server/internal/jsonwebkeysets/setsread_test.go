package jsonwebkeysets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListSets_Empty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	result, err := ti.service.ListSets(readCtx(t, ctx), &gen.ListSetsPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, result.Sets)
}

func TestListSets_ExcludesDeleted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	kept := createSet(t, ctx, ti, "kept", ek.ID)
	doomed := createSet(t, ctx, ti, "doomed", ek.ID)

	require.NoError(t, ti.service.DeleteSet(adminCtx(t, ctx), &gen.DeleteSetPayload{
		ID:           doomed.ID,
		SessionToken: nil,
	}))

	result, err := ti.service.ListSets(readCtx(t, ctx), &gen.ListSetsPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, result.Sets, 1)
	require.Equal(t, kept.ID, result.Sets[0].ID)
}

func TestGetSet_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ek := createBackedGcpKmsKey(t, ctx, ti, "signing-key")
	set := createSet(t, ctx, ti, "primary", ek.ID)

	got, err := ti.service.GetSet(readCtx(t, ctx), &gen.GetSetPayload{
		ID:           set.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, set.ID, got.ID)
	require.Equal(t, "primary", got.Name)
	require.Equal(t, ek.ID, got.ExternalKeyID)
}

func TestGetSet_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetSet(readCtx(t, ctx), &gen.GetSetPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetSet_InvalidID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetSet(readCtx(t, ctx), &gen.GetSetPayload{
		ID:           "not-a-uuid",
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
