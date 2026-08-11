import { type BillingCycle } from "./billing-cycles";

// Contract pricing math for the platform-admin TUM estimator. Encodes the
// 7/30 pricing model so an admin can approximate what an account's contract
// is worth under either commercial shape — a committed platform fee with
// tiered overage, or uncommitted pay-as-you-go.
//
// Nothing here is billed from. These are sales-side approximations off
// observed TUM, kept in a pure module so the numbers are unit-testable and
// the component file stays component-only (react-refresh).

const TOKENS_PER_MILLION = 1_000_000;

// The effective rate a committed baseline works out to: annual platform fee
// divided by committed annual token volume. Used to DERIVE a default platform
// fee from a baseline when the real negotiated fee isn't to hand — the admin
// can always overwrite the fee, which is the number that actually matters.
export const BASELINE_RATE_PER_MILLION = 0.21;

// Overage bands, keyed on MULTIPLES of the monthly baseline — so the band
// boundaries scale with the contract rather than sitting at absolute volumes.
// Rates step down as volume grows, the intent being that no band undercuts
// the committed baseline rate: if one did, under-committing would become the
// cheaper play and the commit model would invert.
//
// Note the 8×+ band at $0.19 does sit under the $0.21 baseline rate, so that
// property does not strictly hold at the very top of the ladder. The rates
// here match the pricing model's table (which the PAYG section cross-
// references as "our lowest contract overage tier ($0.19/M)"); an account
// running 8× its baseline is a renegotiation conversation long before the
// inversion matters in practice.
const OVERAGE_TIERS: {
  label: string;
  upToMultiple: number;
  ratePerMillion: number;
}[] = [
  { label: "1×–2× baseline", upToMultiple: 2, ratePerMillion: 0.26 },
  { label: "2×–4× baseline", upToMultiple: 4, ratePerMillion: 0.23 },
  { label: "4×–8× baseline", upToMultiple: 8, ratePerMillion: 0.21 },
  { label: "8×+ baseline", upToMultiple: Infinity, ratePerMillion: 0.19 },
];

// Pay-as-you-go bands, keyed on absolute monthly volume. The floor rate
// ($0.24) deliberately sits above both the committed baseline rate and the
// lowest contract overage rate, so PAYG is never the cheaper option at
// equivalent volume — it prices flexibility, not discount.
const PAYG_TIERS: {
  label: string;
  upToTokens: number;
  ratePerMillion: number;
}[] = [
  { label: "0–10B", upToTokens: 10e9, ratePerMillion: 0.35 },
  { label: "10B–30B", upToTokens: 30e9, ratePerMillion: 0.3 },
  { label: "30B–75B", upToTokens: 75e9, ratePerMillion: 0.27 },
  { label: "75B+", upToTokens: Infinity, ratePerMillion: 0.24 },
];

// One band's contribution to a monthly bill.
export type TierLine = {
  label: string;
  tokens: number;
  ratePerMillion: number;
  cost: number;
};

// Walks graduated bands: each band charges only the volume that falls inside
// it, so the bill is the sum of the slices — the same arithmetic the pricing
// doc's worked examples use. Bands with no volume are dropped so the UI shows
// only lines that actually cost something.
function graduatedLines(
  tokens: number,
  bands: { label: string; upper: number; ratePerMillion: number }[],
  from: number,
): TierLine[] {
  const lines: TierLine[] = [];
  let lower = from;
  for (const band of bands) {
    const inBand = Math.min(tokens, band.upper) - lower;
    if (inBand > 0) {
      lines.push({
        label: band.label,
        tokens: inBand,
        ratePerMillion: band.ratePerMillion,
        cost: (inBand / TOKENS_PER_MILLION) * band.ratePerMillion,
      });
    }
    if (tokens <= band.upper) break;
    lower = band.upper;
  }
  return lines;
}

export function sumLines(lines: TierLine[]): number {
  return lines.reduce((total, line) => total + line.cost, 0);
}

// The overage bill for one month at `monthlyTokens` against a contracted
// `baselineTokens`. A baseline of zero (or none) leaves every multiple-band
// zero-width, so the model has nothing to price — callers must treat an empty
// result from a missing baseline as "not applicable", not as "$0 of overage".
export function overageLines(
  monthlyTokens: number,
  baselineTokens: number,
): TierLine[] {
  if (baselineTokens <= 0) return [];
  return graduatedLines(
    monthlyTokens,
    OVERAGE_TIERS.map((t) => ({
      label: t.label,
      upper: baselineTokens * t.upToMultiple,
      ratePerMillion: t.ratePerMillion,
    })),
    baselineTokens,
  );
}

// The full pay-as-you-go bill for one month — every token is charged, from
// the first one, with no baseline to subtract.
//
// `rateAdjustPct` scales every band rate by a percentage (+10 → +10% uplift,
// -15 → 15% discount): PAYG deals are negotiated as a swing off the list
// rates, not as four hand-edited band rates. Band boundaries are absolute
// volumes and don't move. A swing at or past -100% would zero or negate
// every rate, so it's treated as invalid and ignored — list rates — the
// same fallback the estimator's input applies, so no layer ever prices
// PAYG as free.
export function paygLines(
  monthlyTokens: number,
  rateAdjustPct = 0,
): TierLine[] {
  const rateMultiplier = rateAdjustPct > -100 ? 1 + rateAdjustPct / 100 : 1;
  return graduatedLines(
    monthlyTokens,
    PAYG_TIERS.map((t) => ({
      label: t.label,
      upper: t.upToTokens,
      ratePerMillion: t.ratePerMillion * rateMultiplier,
    })),
    0,
  );
}

// The platform fee a baseline implies at the model's effective committed
// rate. A starting point for the estimate, not a quote: real contracts are
// negotiated, which is why the admin can override the fee.
export function derivedAnnualPlatformFee(baselineTokens: number): number {
  return (baselineTokens * 12 * BASELINE_RATE_PER_MILLION) / TOKENS_PER_MILLION;
}

// The blended $/M an annual bill works out to at a given monthly volume —
// the single number that makes the two models directly comparable.
export function effectiveRatePerMillion(
  annualCost: number,
  monthlyTokens: number,
): number | null {
  const annualTokens = monthlyTokens * 12;
  if (annualTokens <= 0) return null;
  return annualCost / (annualTokens / TOKENS_PER_MILLION);
}

// Which observed volume the estimate runs on. Contract conversations happen
// against different readings of the same account — what it's doing right now,
// what it did last month, its recent typical, and its worst month — so the
// estimator exposes all four rather than picking one.
export type VolumeBasis = "projected" | "last" | "avg3" | "peak" | "custom";

export type VolumeBasisOption = {
  value: VolumeBasis;
  label: string;
  // Null when the account's history can't support the basis yet (e.g. no
  // completed cycle). The picker disables those rather than hiding them, so
  // the absence is visible instead of looking like the option never existed.
  tokens: number | null;
  hint: string;
};

// How much of a cycle must have elapsed before extrapolating it is worth
// anything. Early in a cycle the multiplier is enormous — on day 2 of a
// 31-day cycle a projection amplifies one quiet weekend fifteen-fold — and
// the result reads as a headline contract number rather than as the noise it
// is. Under this threshold the basis reports as unavailable instead.
const MIN_PROJECTION_ELAPSED_SHARE = 0.25;

// The active cycle's usage extrapolated to a full cycle at its current rate,
// or null while the cycle is too young for that to mean anything.
function projectedCurrent(cycles: BillingCycle[], now: number): number | null {
  const current = cycles.find((c) => c.current);
  if (!current) return null;
  const fullMs = current.end.getTime() - current.start.getTime();
  if (fullMs <= 0) return current.tokens;
  const elapsedMs =
    Math.min(now, current.end.getTime()) - current.start.getTime();
  if (elapsedMs < fullMs * MIN_PROJECTION_ELAPSED_SHARE) return null;
  return Math.round((current.tokens * fullMs) / elapsedMs);
}

// Fewest closed cycles that make the trailing mean a mean at all.
const MIN_AVERAGE_CYCLES = 2;

// Cycles, most recent first, that have closed — the only ones whose totals
// are final enough to average or take a peak from.
function completedCycles(cycles: BillingCycle[]): BillingCycle[] {
  return cycles.filter((c) => !c.current);
}

export function volumeBasisOptions(
  cycles: BillingCycle[],
  now: number,
): VolumeBasisOption[] {
  const completed = completedCycles(cycles);
  const recent = completed.slice(0, 3);
  // A single closed cycle is not an average — that reading is exactly "last
  // full cycle", and offering it under an "Avg." label would imply a
  // smoothing that isn't happening. Two is the smallest honest mean.
  const avgTokens =
    recent.length >= MIN_AVERAGE_CYCLES
      ? Math.round(recent.reduce((s, c) => s + c.tokens, 0) / recent.length)
      : null;
  // The label states how many cycles actually went into the mean, so a
  // two-cycle average never reads as a three-cycle one.
  const avgCount = recent.length >= MIN_AVERAGE_CYCLES ? recent.length : 3;
  const peak =
    completed.length > 0 ? Math.max(...completed.map((c) => c.tokens)) : null;

  return [
    {
      value: "projected",
      label: "Current cycle (projected)",
      tokens: projectedCurrent(cycles, now),
      hint: `The active cycle's usage so far, extrapolated to a full cycle. Withheld until ${Math.round(MIN_PROJECTION_ELAPSED_SHARE * 100)}% of the cycle has elapsed — before that the extrapolation is mostly noise.`,
    },
    {
      value: "last",
      label: "Last full cycle",
      tokens: completed[0]?.tokens ?? null,
      hint: "The most recently closed billing cycle's billed total.",
    },
    {
      value: "avg3",
      label: `Avg. last ${avgCount} cycles`,
      tokens: avgTokens,
      hint:
        avgTokens != null
          ? `Mean of the ${recent.length} most recent closed cycles.`
          : `Needs at least ${MIN_AVERAGE_CYCLES} closed cycles to average — with one, use "Last full cycle".`,
    },
    {
      value: "peak",
      label: "Peak cycle",
      tokens: peak,
      hint: "The heaviest closed cycle on record — the worst-case month.",
    },
    {
      value: "custom",
      label: "Custom volume",
      // Supplied by the caller's own input, not derived from history.
      tokens: null,
      hint: "A hypothetical monthly volume, for sizing a new contract.",
    },
  ];
}

const usdWhole = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 0,
});
const usdCents = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

// Whole dollars once a figure is big enough that cents are noise; cents below
// that, so small line items don't all collapse to "$0".
export function formatUSD(value: number): string {
  return Math.abs(value) >= 1000
    ? usdWhole.format(value)
    : usdCents.format(value);
}

// "+10%" / "-15%": the sign is the information — an unsigned "10%" wouldn't
// say which way the rates moved.
export function formatSignedPct(pct: number): string {
  return `${pct > 0 ? "+" : ""}${pct.toLocaleString("en-US", { maximumFractionDigits: 2 })}%`;
}

// Reads the annual gap between the two models for the estimator's summary
// line. PAYG undercutting the committed contract is a modelling error at
// list rates — and a fortiori under an uplift, which only raises PAYG above
// list — but it's the expected outcome of a big enough negotiated discount.
// So only a discount claims the gap; everything else falls through to the
// check-the-fee warning.
export function paygDeltaMessage(delta: number, rateAdjustPct: number): string {
  if (delta > 0) {
    return `Pay-as-you-go costs ${formatUSD(delta)}/yr more than the committed contract at this volume — the number to point at in a "should we commit?" conversation.`;
  }
  if (delta < 0 && rateAdjustPct < 0) {
    return `Pay-as-you-go is ${formatUSD(-delta)}/yr cheaper here at the adjusted rates (${formatSignedPct(rateAdjustPct)} vs list).`;
  }
  if (delta < 0) {
    return `Pay-as-you-go is ${formatUSD(-delta)}/yr cheaper here, which the model isn't meant to allow — check the platform fee against the baseline.`;
  }
  return "Both models price identically at this volume.";
}

// Token counts at contract scale are billions — the raw digits are unreadable
// in a table of prices, so compact them to the unit the pricing model itself
// is written in.
export function formatTokensCompact(tokens: number): string {
  if (tokens >= 1e9) {
    return `${(tokens / 1e9).toLocaleString("en-US", { maximumFractionDigits: 2 })}B`;
  }
  if (tokens >= 1e6) {
    return `${(tokens / 1e6).toLocaleString("en-US", { maximumFractionDigits: 1 })}M`;
  }
  return tokens.toLocaleString("en-US");
}
