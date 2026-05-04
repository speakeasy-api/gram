package functions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConcurrencyLimits_256MB(t *testing.T) {
	t.Parallel()
	soft, hard := concurrencyLimits(256)
	require.Equal(t, 5, hard)
	require.Equal(t, 2, soft)
}

func TestConcurrencyLimits_512MB(t *testing.T) {
	t.Parallel()
	soft, hard := concurrencyLimits(512)
	require.Equal(t, 10, hard)
	require.Equal(t, 2, soft)
}

func TestConcurrencyLimits_1024MB(t *testing.T) {
	t.Parallel()
	soft, hard := concurrencyLimits(1024)
	require.Equal(t, 21, hard)
	require.Equal(t, 4, soft)
}

func TestConcurrencyLimits_2048MB(t *testing.T) {
	t.Parallel()
	soft, hard := concurrencyLimits(2048)
	require.Equal(t, 42, hard)
	require.Equal(t, 8, soft)
}

func TestConcurrencyLimits_TinyMemory(t *testing.T) {
	t.Parallel()
	soft, hard := concurrencyLimits(64)
	require.Equal(t, 4, hard, "hard limit floors at 4")
	require.Equal(t, 2, soft)
}

func TestConcurrencyLimits_ZeroMemory(t *testing.T) {
	t.Parallel()
	soft, hard := concurrencyLimits(0)
	require.Equal(t, 4, hard, "hard limit floors at 4")
	require.Equal(t, 2, soft, "soft limit floors at 2 because 1/5 of 4 = 0")
}
