package pricing

import (
	"math"
	"testing"
)

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}

func TestPaygMonthlyPrice_ZeroAndNegative(t *testing.T) {
	t.Parallel()

	if got := PaygMonthlyPrice(0); got != 0 {
		t.Fatalf("PaygMonthlyPrice(0) = %v, want 0", got)
	}
	if got := PaygMonthlyPrice(-5); got != 0 {
		t.Fatalf("PaygMonthlyPrice(-5) = %v, want 0", got)
	}
}

func TestPaygMonthlyPrice_FirstBand(t *testing.T) {
	t.Parallel()

	// 1M tokens entirely within the first band: 1M / 1M * $0.35 = $0.35.
	if got := PaygMonthlyPrice(1_000_000); !approxEqual(got, 0.35) {
		t.Fatalf("PaygMonthlyPrice(1M) = %v, want 0.35", got)
	}

	// 1B tokens in the first band: 1000M * $0.35 = $350.
	if got := PaygMonthlyPrice(1_000_000_000); !approxEqual(got, 350) {
		t.Fatalf("PaygMonthlyPrice(1B) = %v, want 350", got)
	}
}

func TestPaygMonthlyPrice_GraduatedAcrossBands(t *testing.T) {
	t.Parallel()

	// 20B tokens: first 10B @ $0.35/M + next 10B @ $0.30/M.
	// = 10_000 * 0.35 + 10_000 * 0.30 = 3500 + 3000 = 6500.
	if got := PaygMonthlyPrice(20_000_000_000); !approxEqual(got, 6500) {
		t.Fatalf("PaygMonthlyPrice(20B) = %v, want 6500", got)
	}

	// 100B tokens: 10B@0.35 + 20B@0.30 + 45B@0.27 + 25B@0.24
	// = 3500 + 6000 + 12150 + 6000 = 27650.
	if got := PaygMonthlyPrice(100_000_000_000); !approxEqual(got, 27650) {
		t.Fatalf("PaygMonthlyPrice(100B) = %v, want 27650", got)
	}
}

func TestPaygEffectiveRatePerMillion(t *testing.T) {
	t.Parallel()

	if got := PaygEffectiveRatePerMillion(0); got != 0 {
		t.Fatalf("PaygEffectiveRatePerMillion(0) = %v, want 0", got)
	}

	// Fully within the first band → blended rate equals the band rate.
	if got := PaygEffectiveRatePerMillion(5_000_000_000); !approxEqual(got, 0.35) {
		t.Fatalf("PaygEffectiveRatePerMillion(5B) = %v, want 0.35", got)
	}

	// 20B → 6500 / 20000M = 0.325 blended.
	if got := PaygEffectiveRatePerMillion(20_000_000_000); !approxEqual(got, 0.325) {
		t.Fatalf("PaygEffectiveRatePerMillion(20B) = %v, want 0.325", got)
	}
}
