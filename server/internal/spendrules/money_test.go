package spendrules_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/spendrules"
)

func TestUSDToCents(t *testing.T) {
	t.Parallel()

	cents, err := spendrules.USDToCents(12.345)
	require.NoError(t, err)
	require.Equal(t, int64(1235), cents)
	require.InDelta(t, 12.35, spendrules.CentsToUSD(cents), 0.001)
}

func TestUSDToCentsRejectsInvalidAmounts(t *testing.T) {
	t.Parallel()

	_, err := spendrules.USDToCents(math.NaN())
	require.Error(t, err)

	_, err = spendrules.USDToCents(math.Inf(1))
	require.Error(t, err)
}
