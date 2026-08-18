import { describe, expect, it } from "vitest";

import { type BillingCycle } from "./billing-cycles";
import {
  derivedAnnualPlatformFee,
  effectiveRatePerMillion,
  formatTokensCompact,
  overageLines,
  paygDeltaMessage,
  paygLines,
  sumLines,
  type VolumeBasisOption,
  volumeBasisOptions,
} from "./contract-pricing";

const B = 1e9;

function cycle(partial: Partial<BillingCycle>): BillingCycle {
  return {
    start: new Date("2026-01-01T00:00:00Z"),
    end: new Date("2026-02-01T00:00:00Z"),
    tokens: 0,
    current: false,
    days: [],
    ...partial,
  };
}

function byBasis(
  options: VolumeBasisOption[],
  value: string,
): VolumeBasisOption | undefined {
  return options.find((o) => o.value === value);
}

describe("pay-as-you-go pricing", () => {
  // The pricing doc's own worked example. If this drifts, the estimator and
  // whatever sales quotes have stopped agreeing.
  it("prices a sustained 80B/month customer at $22,850/month", () => {
    const lines = paygLines(80 * B);
    expect(lines.map((l) => [l.tokens, l.cost])).toEqual([
      [10 * B, 3500],
      [20 * B, 6000],
      [45 * B, 12150],
      [5 * B, 1200],
    ]);
    expect(sumLines(lines)).toBeCloseTo(22850, 6);
  });

  it("prices a 5B/month customer wholly inside tier 1", () => {
    const lines = paygLines(5 * B);
    expect(lines).toHaveLength(1);
    expect(sumLines(lines)).toBeCloseTo(1750, 6);
  });

  it("charges from the first token — no free baseline", () => {
    expect(sumLines(paygLines(1e6))).toBeCloseTo(0.35, 6);
  });

  it("produces no lines at zero volume", () => {
    expect(paygLines(0)).toEqual([]);
  });

  it("stops at the band the volume lands in", () => {
    // Exactly at a boundary the next band contributes nothing, so it must not
    // appear as a zero-token line.
    expect(paygLines(10 * B)).toHaveLength(1);
  });
});

describe("pay-as-you-go rate adjustment", () => {
  it("scales every band rate up by an uplift percentage", () => {
    const adjusted = paygLines(80 * B, 10);
    const list = paygLines(80 * B);
    // The uplift moves rates, never band boundaries — same slices, dearer.
    expect(adjusted.map((l) => l.tokens)).toEqual(list.map((l) => l.tokens));
    adjusted.forEach((line, i) => {
      expect(line.ratePerMillion).toBeCloseTo(
        (list[i]?.ratePerMillion ?? 0) * 1.1,
        9,
      );
    });
    expect(sumLines(adjusted)).toBeCloseTo(22850 * 1.1, 6);
  });

  it("scales every band rate down by a discount percentage", () => {
    expect(sumLines(paygLines(80 * B, -15))).toBeCloseTo(22850 * 0.85, 6);
  });

  it("leaves list rates untouched at zero adjustment", () => {
    expect(paygLines(80 * B, 0)).toEqual(paygLines(80 * B));
  });

  it("ignores swings at or past -100% and prices at list rates", () => {
    // -100% would zero every rate and anything past it would go negative.
    // Both are invalid input, and the pure function applies the same
    // fallback as the estimator's input field — list rates — so no layer
    // ever prices PAYG as free.
    expect(paygLines(80 * B, -100)).toEqual(paygLines(80 * B));
    expect(paygLines(80 * B, -150)).toEqual(paygLines(80 * B));
  });
});

describe("paygDeltaMessage", () => {
  it("frames a dearer PAYG as the case for committing", () => {
    expect(paygDeltaMessage(5000, 0)).toMatch(/costs \$5,000\/yr more/);
  });

  it("flags a cheaper PAYG at list rates as a modelling error", () => {
    expect(paygDeltaMessage(-5000, 0)).toMatch(/check the platform fee/);
  });

  it("attributes a cheaper PAYG under a discount to the discount", () => {
    const message = paygDeltaMessage(-5000, -15);
    expect(message).toMatch(/adjusted rates \(-15% vs list\)/);
    expect(message).not.toMatch(/check the platform fee/);
  });

  it("keeps the modelling-error warning under an uplift", () => {
    // An uplift only raises PAYG above list, so PAYG still undercutting the
    // committed model is a stronger anomaly signal, not a discount story.
    expect(paygDeltaMessage(-5000, 10)).toMatch(/check the platform fee/);
  });

  it("keeps the modelling-error warning for out-of-window swings", () => {
    // paygLines ignores swings at or past -100% and prices at list rates,
    // so the message must not credit an adjustment that was never applied.
    expect(paygDeltaMessage(-5000, -100)).toMatch(/check the platform fee/);
    expect(paygDeltaMessage(-5000, -150)).toMatch(/check the platform fee/);
  });

  it("reports an exact tie as identical pricing", () => {
    expect(paygDeltaMessage(0, 0)).toMatch(/identically/);
  });
});

describe("committed-contract overage pricing", () => {
  it("charges the first-tier rate between 1x and 2x baseline", () => {
    const lines = overageLines(15 * B, 10 * B);
    expect(lines).toEqual([
      {
        label: "1×–2× baseline",
        tokens: 5 * B,
        ratePerMillion: 0.26,
        cost: 1300,
      },
    ]);
  });

  it("graduates across bands as usage climbs past multiples of baseline", () => {
    const lines = overageLines(50 * B, 10 * B);
    expect(lines.map((l) => [l.tokens, l.cost])).toEqual([
      [10 * B, 2600], // 1x–2x  @ $0.26
      [20 * B, 4600], // 2x–4x  @ $0.23
      [10 * B, 2100], // 4x–8x  @ $0.21, capped at 5x
    ]);
    expect(sumLines(lines)).toBeCloseTo(9300, 6);
  });

  it("steps rates down monotonically, never below the $0.19 floor", () => {
    const rates = overageLines(100 * B, 1 * B).map((l) => l.ratePerMillion);
    expect(rates).toEqual([0.26, 0.23, 0.21, 0.19]);
    for (const rate of rates) expect(rate).toBeGreaterThanOrEqual(0.19);
  });

  it("prices unplanned usage at a premium over the committed rate", () => {
    // Tier 1 is where nearly all overage lands, and it must stay above the
    // baseline rate — otherwise under-committing becomes the cheaper play.
    const [first] = overageLines(15 * B, 10 * B);
    expect(first?.ratePerMillion ?? 0).toBeGreaterThan(0.21);
  });

  it("charges nothing while usage sits under the baseline", () => {
    expect(overageLines(8 * B, 10 * B)).toEqual([]);
  });

  it("has nothing to price without a baseline", () => {
    // Every band is a multiple of the baseline, so a zero baseline leaves them
    // all zero-width — the caller must read this as "not applicable".
    expect(overageLines(50 * B, 0)).toEqual([]);
  });
});

describe("derived platform fee", () => {
  it("reproduces the doc's ~$189k contract for a 75B baseline", () => {
    expect(derivedAnnualPlatformFee(75 * B)).toBeCloseTo(189_000, 6);
  });

  it("keeps a committed contract cheaper than PAYG at the same volume", () => {
    // The whole point of the tier design: at equal volume, committing wins.
    const volume = 80 * B;
    const committed =
      derivedAnnualPlatformFee(75 * B) +
      sumLines(overageLines(volume, 75 * B)) * 12;
    expect(committed).toBeLessThan(sumLines(paygLines(volume)) * 12);
  });
});

describe("effectiveRatePerMillion", () => {
  it("blends an annual bill back to a $/M rate", () => {
    // $0.30/M across 12 months of 1B tokens.
    expect(effectiveRatePerMillion(3600, 1 * B)).toBeCloseTo(0.3, 6);
  });

  it("is undefined at zero volume rather than infinite", () => {
    expect(effectiveRatePerMillion(1000, 0)).toBeNull();
  });
});

describe("volumeBasisOptions", () => {
  const now = Date.UTC(2026, 0, 16); // Halfway through a 31-day January cycle.

  it("projects the active cycle to a full cycle at its current rate", () => {
    const options = volumeBasisOptions(
      [cycle({ tokens: 5 * B, current: true })],
      now,
    );
    const projected = byBasis(options, "projected")?.tokens ?? 0;
    // 15 of 31 days elapsed, so the projection roughly doubles the total.
    expect(projected).toBeGreaterThan(10 * B);
    expect(projected).toBeLessThan(11 * B);
  });

  it("withholds the projection until a quarter of the cycle has elapsed", () => {
    // Day 2 of a 31-day cycle: extrapolating here multiplies a quiet weekend
    // into a headline contract number, so the basis reports unavailable.
    const start = Date.UTC(2026, 0, 1);
    const options = volumeBasisOptions(
      [
        cycle({
          tokens: 1 * B,
          current: true,
          start: new Date(start),
          end: new Date(Date.UTC(2026, 1, 1)),
        }),
      ],
      Date.UTC(2026, 0, 3),
    );
    expect(byBasis(options, "projected")?.tokens).toBeNull();
  });

  it("averages and peaks over closed cycles only", () => {
    const cycles = [
      cycle({ tokens: 100 * B, current: true }),
      cycle({ tokens: 10 * B }),
      cycle({ tokens: 20 * B }),
      cycle({ tokens: 30 * B }),
      // Older than the trailing-3 window: peaks but never averages.
      cycle({ tokens: 90 * B }),
    ];
    const options = volumeBasisOptions(cycles, now);
    expect(byBasis(options, "last")?.tokens).toBe(10 * B);
    expect(byBasis(options, "avg3")?.tokens).toBe(20 * B);
    expect(byBasis(options, "peak")?.tokens).toBe(90 * B);
  });

  it("withholds the average until there are two cycles to average", () => {
    // One closed cycle is not a mean — it is exactly "last full cycle", and
    // labelling it as an average would imply smoothing that isn't happening.
    const options = volumeBasisOptions(
      [cycle({ tokens: 5 * B, current: true }), cycle({ tokens: 10 * B })],
      now,
    );
    expect(byBasis(options, "avg3")?.tokens).toBeNull();
    expect(byBasis(options, "last")?.tokens).toBe(10 * B);
  });

  it("labels the average with the number of cycles actually in it", () => {
    const twoCycles = volumeBasisOptions(
      [
        cycle({ tokens: 5 * B, current: true }),
        cycle({ tokens: 10 * B }),
        cycle({ tokens: 20 * B }),
      ],
      now,
    );
    expect(byBasis(twoCycles, "avg3")?.label).toBe("Avg. last 2 cycles");
    expect(byBasis(twoCycles, "avg3")?.tokens).toBe(15 * B);

    const threeCycles = volumeBasisOptions(
      [
        cycle({ tokens: 10 * B }),
        cycle({ tokens: 20 * B }),
        cycle({ tokens: 30 * B }),
      ],
      now,
    );
    expect(byBasis(threeCycles, "avg3")?.label).toBe("Avg. last 3 cycles");
  });

  it("reports history-derived bases as unavailable on a brand-new account", () => {
    const options = volumeBasisOptions(
      [cycle({ tokens: 0, current: true })],
      now,
    );
    expect(byBasis(options, "last")?.tokens).toBeNull();
    expect(byBasis(options, "avg3")?.tokens).toBeNull();
    expect(byBasis(options, "peak")?.tokens).toBeNull();
  });
});

describe("formatTokensCompact", () => {
  it("renders contract-scale volumes in the units the model is written in", () => {
    expect(formatTokensCompact(80 * B)).toBe("80B");
    expect(formatTokensCompact(1.5e6)).toBe("1.5M");
    expect(formatTokensCompact(500)).toBe("500");
  });
});
