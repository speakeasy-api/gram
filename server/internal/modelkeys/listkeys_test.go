package modelkeys_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/model_keys"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
)

func TestListKeys_EmptyProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	list, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Empty(t, list.Keys)
}

func TestListKeys_ReturnsConfiguredKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	_, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, nil))
	require.NoError(t, err)
	_, err = ti.service.UpsertKey(ctx, newUpsertPayload("assistants", nil))
	require.NoError(t, err)

	list, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, list.Keys, 2)

	slots := []string{list.Keys[0].Slot, list.Keys[1].Slot}
	require.ElementsMatch(t, []string{modelkeys.SlotDefault, "assistants"}, slots)
}
