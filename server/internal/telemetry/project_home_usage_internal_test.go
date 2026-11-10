package telemetry

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompareMul128(t *testing.T) {
	t.Parallel()

	require.Equal(t, 0, compareMul128(2, 3, 3, 2))
	require.Equal(t, 1, compareMul128(3, 3, 2, 4))
	require.Equal(t, -1, compareMul128(1, 2, 1, 3))
	// Products beyond uint64 still compare correctly.
	require.Equal(t, 1, compareMul128(math.MaxUint64, 3, math.MaxUint64, 2))
	require.Equal(t, -1, compareMul128(math.MaxUint64, math.MaxUint64-1, math.MaxUint64, math.MaxUint64))
}
