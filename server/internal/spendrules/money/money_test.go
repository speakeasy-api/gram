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

func TestFromUSDRejectsCentOverflow(t *testing.T) {
	t.Parallel()

	// float64(math.MaxInt64) rounds up past MaxInt64, so amounts at the
	// boundary must be rejected or the cents conversion would wrap negative.
	maxUSD := float64(math.MaxInt64) / 100

	_, err := money.FromUSD(maxUSD)
	require.Error(t, err)

	// -maxUSD converts to exactly math.MinInt64 and is representable; only
	// amounts strictly below it overflow.
	_, err = money.FromUSD(math.Nextafter(-maxUSD, math.Inf(-1)))
	require.Error(t, err)

	minCents, err := money.FromUSD(-maxUSD)
	require.NoError(t, err)
	require.Equal(t, money.Cents(math.MinInt64), minCents)

	cents, err := money.FromUSD(math.Nextafter(maxUSD, 0))
	require.NoError(t, err)
	require.Positive(t, cents)
}
