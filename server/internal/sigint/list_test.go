package sigint_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
)

func TestListSignalsCursorSurvivesDeletionWithoutDroppingRows(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ids := make([]string, 5)
	for i := range ids {
		ids[i] = createSignal(t, ctx, ti, fmt.Sprintf("signal %d", i)).ID
	}
	slices.Sort(ids)
	_, err := ti.service.DeleteSignal(ctx, &gen.DeleteSignalPayload{
		ID: ids[2], SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	first, err := ti.service.ListSignals(ctx, &gen.ListSignalsPayload{
		Cursor: nil, Limit: 2, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, first.Signals, 2)
	require.Equal(t, []string{ids[0], ids[1]}, []string{first.Signals[0].ID, first.Signals[1].ID})
	require.Equal(t, &ids[1], first.NextCursor)

	_, err = ti.service.DeleteSignal(ctx, &gen.DeleteSignalPayload{
		ID: ids[1], SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	second, err := ti.service.ListSignals(ctx, &gen.ListSignalsPayload{
		Cursor: first.NextCursor, Limit: 2, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, second.Signals, 2)
	require.Equal(t, []string{ids[3], ids[4]}, []string{second.Signals[0].ID, second.Signals[1].ID})
	require.Nil(t, second.NextCursor)
}
