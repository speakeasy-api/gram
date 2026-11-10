package contextvalues

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppendConnectionReference_UpstreamLookAlikeDoesNotSuppressOurs(t *testing.T) {
	t.Parallel()

	ctx := WithConnectionReferenceSlot(t.Context())
	SetConnectionReference(ctx, "ours")

	require.Equal(t, "Reference: other\nReference: ours", AppendConnectionReference(ctx, "Reference: other"))
	require.Equal(t, "Reference: ours is mentioned\nReference: ours", AppendConnectionReference(ctx, "Reference: ours is mentioned"))
	require.Equal(t, "text\nReference: ours", AppendConnectionReference(ctx, "text\nReference: ours"), "already added at the end")
}

func TestReplaceConnectionReference_ClearsAConflict(t *testing.T) {
	t.Parallel()

	ctx := WithConnectionReferenceSlot(t.Context())
	SetConnectionReference(ctx, "a")
	SetConnectionReference(ctx, "b")
	require.Empty(t, ConnectionReference(ctx))

	ReplaceConnectionReference(ctx, "c")
	require.Equal(t, "c", ConnectionReference(ctx))
}

func TestSetConnectionReference_AFailureWithNoIDShowsNone(t *testing.T) {
	t.Parallel()

	ctx := WithConnectionReferenceSlot(t.Context())
	SetConnectionReference(ctx, "a")
	SetConnectionReference(ctx, "")
	require.Empty(t, ConnectionReference(ctx))

	SetConnectionReference(ctx, "b")
	require.Empty(t, ConnectionReference(ctx), "a later id can't undo the doubt")
}
