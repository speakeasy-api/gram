import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import { useMemo, useState } from "react";
import { type BillingCycle } from "./billing-cycles";
import {
  BASELINE_RATE_PER_MILLION,
  derivedAnnualPlatformFee,
  effectiveRatePerMillion,
  formatSignedPct,
  formatTokensCompact,
  formatUSD,
  overageLines,
  paygDeltaMessage,
  paygLines,
  sumLines,
  type TierLine,
  type VolumeBasis,
  volumeBasisOptions,
} from "./contract-pricing";

// The per-band table both model cards share: what volume landed in each tier,
// at what rate, for how much. Showing the bands rather than one lump sum is
// the point — it's what an account team reads off when a customer asks why
// the number is what it is.
function TierTable({
  caption,
  lines,
}: {
  // Names what the bands are measuring. A real <caption> rather than a
  // heading above the table, so it reaches assistive tech as the table's
  // label instead of as unrelated preceding text.
  caption: string;
  lines: TierLine[];
}): JSX.Element {
  if (lines.length === 0) {
    return (
      <>
        <div className="text-muted-foreground mb-1 text-xs">{caption}</div>
        <Text muted small>
          No volume in any tier.
        </Text>
      </>
    );
  }
  return (
    <table className="w-full text-sm tabular-nums">
      <caption className="text-muted-foreground mb-1 text-left text-xs">
        {caption}
      </caption>
      {/* The columns are only distinguishable by position and formatting,
          which a screen reader can't convey — so the headers exist for it
          and are hidden from the compact visual layout. */}
      <thead className="sr-only">
        <tr>
          <th scope="col">Tier</th>
          <th scope="col">Volume</th>
          <th scope="col">Rate</th>
          <th scope="col">Cost</th>
        </tr>
      </thead>
      <tbody>
        {lines.map((line) => (
          <tr key={line.label} className="text-muted-foreground">
            <th scope="row" className="py-0.5 pr-2 text-left font-normal">
              {line.label}
            </th>
            <td className="py-0.5 pr-2 text-right">
              {formatTokensCompact(line.tokens)}
            </td>
            <td className="py-0.5 pr-2 text-right">
              ${line.ratePerMillion.toFixed(2)}/M
            </td>
            <td className="text-foreground py-0.5 text-right font-medium">
              {formatUSD(line.cost)}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// One model's bottom line. The annual figure is the headline because that's
// the unit contracts are written and renewed in; the monthly sits under it
// for reconciling against a single invoice.
function ModelTotals({
  monthly,
  annual,
  rate,
}: {
  monthly: number;
  annual: number;
  rate: number | null;
}): JSX.Element {
  return (
    <div className="border-border mt-3 border-t pt-3">
      <div className="flex items-baseline justify-between">
        <span className="text-muted-foreground text-xs">Annual</span>
        <span className="text-xl font-semibold tabular-nums">
          {formatUSD(annual)}
        </span>
      </div>
      <div className="text-muted-foreground mt-1 flex items-baseline justify-between text-xs tabular-nums">
        <span>{formatUSD(monthly)}/mo</span>
        {rate != null && <span>${rate.toFixed(3)}/M blended</span>}
      </div>
    </div>
  );
}

function ModelCard({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="border-border border p-4">
      <div className="mb-3">
        <div className="text-sm font-medium">{title}</div>
        <div className="text-muted-foreground text-xs">{subtitle}</div>
      </div>
      {children}
    </div>
  );
}

// How hard the account team should be looking at this contract. Steady
// accounts run overage at a fraction of their base; once overage rivals the
// base contract the customer is better off upsizing the baseline than paying
// the premium, and that conversation is better had by us than by their
// finance team.
function overageSignal(share: number): {
  tone: "muted" | "warning" | "destructive";
  message: string;
} {
  const pct = Math.round(share * 100);
  if (share >= 1) {
    return {
      tone: "destructive",
      message: `Overage is ${pct}% of the base contract — size up the baseline at renewal.`,
    };
  }
  if (share >= 0.5) {
    return {
      tone: "warning",
      message: `Overage is ${pct}% of the base contract — worth an expansion conversation.`,
    };
  }
  return {
    tone: "muted",
    message: `Overage is ${pct}% of the base contract — the account is well-sized.`,
  };
}

/**
 * Approximates what an account is worth under each of the two commercial
 * shapes — a committed platform fee with tiered overage, and uncommitted
 * pay-as-you-go — off the org's observed tokens under management.
 *
 * Estimate only: nothing here is billed from and nothing is persisted. The
 * platform fee in particular is a negotiated number, so it's an override the
 * admin types, defaulted from the baseline at the model's effective rate.
 */
export function ContractPriceEstimator({
  baselineTokens,
  cycles,
}: {
  // The contracted monthly baseline. Read live off the contract-terms form
  // above, so an admin sizing a new limit sees the price move as they type.
  // Null when the org has no contracted limit — the committed model has no
  // tier boundaries without one.
  baselineTokens: number | null;
  cycles: BillingCycle[];
}): JSX.Element {
  const [basisOverride, setBasisOverride] = useState<VolumeBasis | null>(null);
  const [customVolume, setCustomVolume] = useState("");
  const [feeOverride, setFeeOverride] = useState("");
  const [paygAdjustment, setPaygAdjustment] = useState("");

  // Deliberately not memoized on `cycles`. Whether the projection is even
  // offered depends on how much of the cycle has elapsed, so a `now` captured
  // once and cached behind a `cycles` dependency would freeze that answer for
  // as long as the tab stays open. This is a handful of array passes over at
  // most a year of cycles — cheaper than the staleness it would buy.
  const options = volumeBasisOptions(cycles, Date.now());

  // Land on the steadiest reading the account can actually support, rather
  // than pinning a basis that may have no data: a multi-cycle mean smooths
  // the spikes that make a single month misleading, and the projection is a
  // last resort because it's the noisiest of the three. Derived, not stored,
  // so an account that gains history stops defaulting to a weaker basis.
  let defaultBasis: VolumeBasis = "custom";
  for (const candidate of ["avg3", "last", "projected"] as const) {
    if (options.find((o) => o.value === candidate)?.tokens != null) {
      defaultBasis = candidate;
      break;
    }
  }
  const basis = basisOverride ?? defaultBasis;

  const parsedCustom = Number(customVolume);
  const customTokens =
    customVolume.trim() !== "" &&
    Number.isFinite(parsedCustom) &&
    parsedCustom >= 0
      ? parsedCustom
      : null;

  const selected = options.find((o) => o.value === basis);
  const monthlyTokens =
    (basis === "custom" ? customTokens : (selected?.tokens ?? null)) ?? null;

  const parsedFee = Number(feeOverride);
  const derivedFee =
    baselineTokens != null ? derivedAnnualPlatformFee(baselineTokens) : 0;
  const platformFeeAnnual =
    feeOverride.trim() !== "" && Number.isFinite(parsedFee) && parsedFee >= 0
      ? parsedFee
      : derivedFee;

  // Negotiated swing on the PAYG list rates. Bounded below at -100% — past
  // that a rate would go negative — and an invalid or out-of-range entry
  // falls back to list rates rather than blanking the card mid-keystroke.
  const parsedAdjust = Number(paygAdjustment);
  const paygRateAdjustPct =
    paygAdjustment.trim() !== "" &&
    Number.isFinite(parsedAdjust) &&
    parsedAdjust > -100
      ? parsedAdjust
      : 0;

  const committed = useMemo(() => {
    if (
      monthlyTokens == null ||
      baselineTokens == null ||
      baselineTokens <= 0
    ) {
      return null;
    }
    const lines = overageLines(monthlyTokens, baselineTokens);
    const overageMonthly = sumLines(lines);
    const annual = platformFeeAnnual + overageMonthly * 12;
    return {
      lines,
      overageMonthly,
      annual,
      monthly: annual / 12,
      rate: effectiveRatePerMillion(annual, monthlyTokens),
      // Zero-fee contracts would divide by zero; treat them as unsignalled
      // rather than as infinite overage.
      share:
        platformFeeAnnual > 0
          ? (overageMonthly * 12) / platformFeeAnnual
          : null,
    };
  }, [monthlyTokens, baselineTokens, platformFeeAnnual]);

  const payg = useMemo(() => {
    if (monthlyTokens == null) return null;
    const lines = paygLines(monthlyTokens, paygRateAdjustPct);
    const monthly = sumLines(lines);
    const annual = monthly * 12;
    return {
      lines,
      monthly,
      annual,
      rate: effectiveRatePerMillion(annual, monthlyTokens),
    };
  }, [monthlyTokens, paygRateAdjustPct]);

  const signal =
    committed?.share != null ? overageSignal(committed.share) : null;
  const delta = committed && payg ? payg.annual - committed.annual : null;

  return (
    <Stack gap={4}>
      <Stack gap={1}>
        <Text variant="body" className="font-medium">
          Estimated contract value
        </Text>
        <Text muted small>
          Approximates this account under both commercial models off its
          observed TUM. Estimate only — nothing here is billed from or saved.
        </Text>
      </Stack>

      <div className="flex flex-wrap items-end gap-4">
        <Stack gap={2}>
          <Label htmlFor="contract-volume-basis">Monthly volume basis</Label>
          <Select
            value={basis}
            onValueChange={(value) => setBasisOverride(value as VolumeBasis)}
          >
            <SelectTrigger id="contract-volume-basis" className="w-60">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {options.map((option) => (
                <SelectItem
                  key={option.value}
                  value={option.value}
                  // History-derived bases the account can't support yet stay
                  // visible but unselectable, so the gap is legible.
                  disabled={option.value !== "custom" && option.tokens == null}
                >
                  {option.label}
                  {option.value !== "custom" && option.tokens != null
                    ? ` — ${formatTokensCompact(option.tokens)}`
                    : ""}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Stack>

        {basis === "custom" && (
          <Stack gap={2}>
            <Label htmlFor="contract-custom-volume">
              Monthly volume (tokens)
            </Label>
            <Input
              id="contract-custom-volume"
              type="number"
              min={0}
              placeholder="e.g. 30000000000"
              value={customVolume}
              onChange={setCustomVolume}
            />
          </Stack>
        )}

        <Stack gap={2}>
          <Label htmlFor="contract-platform-fee">Annual platform fee ($)</Label>
          <Input
            id="contract-platform-fee"
            type="number"
            min={0}
            placeholder={
              baselineTokens != null && baselineTokens > 0
                ? Math.round(derivedFee).toString()
                : "Set a baseline first"
            }
            value={feeOverride}
            onChange={setFeeOverride}
          />
        </Stack>

        <SimpleTooltip
          tooltip={`Defaults to the baseline priced at the model's committed effective rate: baseline × 12 months × $${BASELINE_RATE_PER_MILLION.toFixed(2)}/M. Override it with the actual negotiated fee.`}
        >
          <span className="text-muted-foreground pb-2.5 text-xs">
            How is the fee defaulted?
          </span>
        </SimpleTooltip>

        <Stack gap={2}>
          <Label htmlFor="contract-payg-adjustment">
            PAYG rate adjustment (%)
          </Label>
          <Input
            id="contract-payg-adjustment"
            type="number"
            min={-100}
            placeholder="e.g. 10 or -15"
            value={paygAdjustment}
            onChange={setPaygAdjustment}
          />
        </Stack>

        <SimpleTooltip tooltip="Scales every pay-as-you-go band rate by this percentage — positive for an uplift, negative for a discount. Band boundaries don't move, and the committed model is unaffected. Estimate only.">
          <span className="text-muted-foreground pb-2.5 text-xs">
            How does the adjustment apply?
          </span>
        </SimpleTooltip>
      </div>

      {/* What the selected basis actually measures. Load-bearing for the
          projection, whose availability rule is otherwise invisible. */}
      {selected && (
        <Text muted small>
          {selected.hint}
        </Text>
      )}

      {monthlyTokens == null ? (
        <Text muted small>
          {basis === "custom"
            ? "Enter a monthly volume to estimate."
            : "This basis has nothing to measure yet — pick another, or enter a custom volume."}
        </Text>
      ) : (
        <>
          <Text muted small>
            Estimating at {formatTokensCompact(monthlyTokens)} tokens/month (
            {formatTokensCompact(monthlyTokens * 12)}/year).
          </Text>

          <div className="grid gap-4 md:grid-cols-2">
            <ModelCard
              title="Platform + Overage"
              subtitle={
                baselineTokens != null && baselineTokens > 0
                  ? `Committed baseline ${formatTokensCompact(baselineTokens)}/mo`
                  : "Requires a contracted baseline"
              }
            >
              {committed ? (
                <>
                  <div className="mb-2 flex items-baseline justify-between text-sm tabular-nums">
                    <span className="text-muted-foreground">
                      Platform fee (annual)
                    </span>
                    <span className="font-medium">
                      {formatUSD(platformFeeAnnual)}
                    </span>
                  </div>
                  <TierTable
                    caption="Monthly overage"
                    lines={committed.lines}
                  />
                  <ModelTotals
                    monthly={committed.monthly}
                    annual={committed.annual}
                    rate={committed.rate}
                  />
                </>
              ) : (
                <Text muted small>
                  Set an allowed TUM per month above — the overage tiers are
                  multiples of the baseline, so there's nothing to price without
                  one.
                </Text>
              )}
            </ModelCard>

            <ModelCard
              title="Pay As You Go"
              subtitle={
                paygRateAdjustPct === 0
                  ? "No commitment — every token billed by volume tier"
                  : `No commitment — volume-tier rates ${formatSignedPct(paygRateAdjustPct)} vs list`
              }
            >
              {payg && (
                <>
                  <TierTable
                    caption="Monthly volume tiers"
                    lines={payg.lines}
                  />
                  <ModelTotals
                    monthly={payg.monthly}
                    annual={payg.annual}
                    rate={payg.rate}
                  />
                </>
              )}
            </ModelCard>
          </div>

          {signal && (
            <div
              className={cn(
                "text-sm",
                signal.tone === "destructive" && "text-destructive",
                signal.tone === "warning" && "text-warning",
                signal.tone === "muted" && "text-muted-foreground",
              )}
            >
              {signal.message}
            </div>
          )}

          {delta != null && (
            <Text muted small>
              {paygDeltaMessage(delta, paygRateAdjustPct)}
            </Text>
          )}
        </>
      )}
    </Stack>
  );
}
