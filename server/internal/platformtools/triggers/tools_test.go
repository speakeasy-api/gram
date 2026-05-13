package triggers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeTargetKindRequiresExplicitValue(t *testing.T) {
	t.Parallel()

	got, err := normalizeTargetKind("")

	require.Error(t, err)
	require.Empty(t, got)
	require.ErrorContains(t, err, "target_kind is required")
}

func TestNormalizeTargetKindAcceptsNoop(t *testing.T) {
	t.Parallel()

	got, err := normalizeTargetKind(targetKindNoop)

	require.NoError(t, err)
	require.Equal(t, targetKindNoop, got)
}
