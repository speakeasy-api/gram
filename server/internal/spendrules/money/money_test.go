package money_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/spendrules/money"
)

func TestFromUSD(t *testing.T) {
	t.Parallel()

	cents, err := money.FromUSD(12.345)
	require.NoError(t, err)
	require.Equal(t, money.Cents(1235), cents)
	require.InDelta(t, 12.35, cents.USD(), 0.001)
}

func TestFromUSDRejectsInvalidAmounts(t *testing.T) {
	t.Parallel()

	_, err := money.FromUSD(math.NaN())
	require.Error(t, err)

	_, err = money.FromUSD(math.Inf(1))
	require.Error(t, err)
}
