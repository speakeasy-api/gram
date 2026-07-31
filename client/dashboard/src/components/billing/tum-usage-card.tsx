import { useMemo, useState } from "react";
import { Info } from "lucide-react";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";
import { ToggleButton } from "@/components/ui/ToggleButton";
import { useOrganization } from "@/contexts/Auth";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useQuery } from "@tanstack/react-query";
import {
  type BilledDays,
  type BillingCycle,
  type BillingPeriod,
  formatCycleName,
  formatPeriodLabel,
  periodCoveredByBilled,
  sumDaysInPeriod,
} from "./billing-cycles";
import { tumDetailsQuery } from "./tum-queries";

const HOUR_MS = 60 * 60 * 1000;

// The selectable units for the average-rate stat.
type AverageUnit = "hour" | "day" | "week";

const AVERAGE_UNITS: { unit: AverageUnit; ms: number }[] = [
  { unit: "hour", ms: HOUR_MS },
  { unit: "day", ms: 24 * HOUR_MS },
  { unit: "week", ms: 7 * 24 * HOUR_MS },
];

// The window the period's averages describe: the full window once it has
// passed, the elapsed portion while it is still underway (a whole-window
// denominator would dilute the rate with time that hasn't happened yet).
// Clamped to at least an hour so a just-opened window doesn't extrapolate
// absurd rates.
function averagingWindowMs(period: BillingPeriod, now: number): number {
  const end = Math.min(now, period.end.getTime());
  return Math.max(end - period.start.getTime(), HOUR_MS);
}

// One average-rate figure with a unit switcher: the period's tokens expressed
// per hour / day / week, defaulting to per day.
function AverageStat({
  period,
  tokens,
}: {
  period: BillingPeriod;
  // Billed tokens within the period; null while the total is still loading.
  tokens: number | null;
}): JSX.Element {
  const [unit, setUnit] = useState<AverageUnit>("day");
  const now = Date.now();
  const windowMs = averagingWindowMs(period, now);
  const unitMs = AVERAGE_UNITS.find((u) => u.unit === unit)?.ms ?? HOUR_MS;
  const average =
    tokens != null ? Math.round((tokens * unitMs) / windowMs) : null;
  const stillRunning = period.end.getTime() > now;
  return (
    <div className="flex flex-col gap-0.5">
      {/* Pinned to the text-xs line height so the pill's chrome overflows
          the label row instead of inflating it — this stat's label and value
          stay aligned with the neighboring Stats. */}
      <span className="text-muted-foreground flex h-4 items-center gap-1 text-xs">
        Avg per
        {/* Bordered so the units read as a clickable segmented control
            rather than as part of the label text. */}
        <span className="border-border flex items-center rounded-md border p-0.5">
          {AVERAGE_UNITS.map((u) => (
            <ToggleButton
              key={u.unit}
              active={unit === u.unit}
              onClick={() => setUnit(u.unit)}
            >
              {u.unit}
            </ToggleButton>
          ))}
        </span>
        <SimpleTooltip
          tooltip={
            stillRunning
              ? "Averaged over the elapsed portion of the selected period."
              : "Averaged over the full selected period."
          }
        >
          {/* A real button so the explanation is reachable by keyboard
              focus, not just pointer hover. */}
          <button
            type="button"
            aria-label="How the average is calculated"
            className="inline-flex shrink-0 cursor-help"
          >
            <Info aria-hidden className="size-3" />
          </button>
        </SimpleTooltip>
      </span>
      {average != null ? (
        <span className="text-xl font-semibold tabular-nums">
          {average.toLocaleString()}
        </span>
      ) : (
        <Skeleton className="h-7 w-24" />
      )}
    </div>
  );
}

// One headline figure in the TUM usage card. The tone ties the number to its
// meter segment: green for the included allowance, amber for overage. A null
// value renders a loading skeleton.
function Stat({
  label,
  value,
  tone,
  hint,
}: {
  label: string;
  value: string | null;
  tone?: "success" | "warning";
  // Optional explanation surfaced as an info tooltip beside the label.
  hint?: string;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-muted-foreground flex h-4 items-center gap-1 text-xs">
        {label}
        {hint && (
          <SimpleTooltip tooltip={hint}>
            <button
              type="button"
              aria-label={`About ${label}`}
              className="inline-flex shrink-0 cursor-help"
            >
              <Info aria-hidden className="size-3" />
            </button>
          </SimpleTooltip>
        )}
      </span>
      {value != null ? (
        <span
          className={cn(
            "text-xl font-semibold tabular-nums",
            // The success text tokens wash out here: near-white in dark mode,
            // muted olive in light. Pin the palette steps that read as green in
            // each mode.
            tone === "success" &&
              "text-[var(--color-feedback-green-600)] dark:text-[var(--color-feedback-green-400)]",
            tone === "warning" && "text-warning",
          )}
        >
          {value}
        </span>
      ) : (
        <Skeleton className="h-7 w-28" />
      )}
    </div>
  );
}

// The meter spans max(usage, allowance): under the cap it reads as a
// fill-toward-the-limit bar; over it, the amber overage segment grows.
function meterShares(
  tokens: number,
  limit: number,
): { included: number; overage: number } {
  const over = Math.max(0, tokens - limit);
  const scale = Math.max(tokens, limit);
  if (scale <= 0) return { included: 0, overage: 0 };
  return {
    included: (Math.min(tokens, limit) / scale) * 100,
    overage: (over / scale) * 100,
  };
}

// Why the Overage figure reads the way it does on a custom range. Full
// cycles carry the plain billed number and need no hint.
function overageHintFor(
  fullCycle: BillingCycle | null,
  overage: number | null,
): string | undefined {
  if (fullCycle) return undefined;
  if (overage == null) {
    return "Overage can't be attributed for this range — it cannot be fully represented by the billed daily data.";
  }
  return "Tokens in this range recorded after the organization's cumulative cycle usage crossed the included allowance. The crossing day is prorated.";
}

/**
 * Resolves the usage card's figures for the effective period. Full cycles
 * render their billed position directly; custom ranges sum the billed daily
 * series when it covers every day of the range (falling back to the shared
 * details query's analytics total when it doesn't) and attribute overage by
 * the covered cycles' allowance-crossing days — the same numbers the details
 * table's Overage column pins to.
 */
export function PeriodUsageCard({
  period,
  cycles,
  billedDays,
  overageDays,
  limit,
}: {
  period: BillingPeriod;
  cycles: BillingCycle[];
  billedDays: BilledDays;
  // Per-day billed overage across covered cycles; null when no allowance.
  overageDays: Map<string, number> | null;
  limit: number | null;
}): JSX.Element {
  const client = useGramContext();
  const organization = useOrganization();
  const fullCycle = period.cycle;
  const covered = periodCoveredByBilled(period, billedDays.covered);
  // Fallback total for ranges the billed daily series can't answer — the
  // same request (identical query key) the chart and details table share.
  const { data: details } = useQuery(
    tumDetailsQuery({ client, orgId: organization.id, period }),
  );

  // The cycle the period sits inside, for the title's context suffix.
  const containingCycle = useMemo(
    () =>
      fullCycle ??
      cycles.find(
        (c) =>
          c.start.getTime() <= period.start.getTime() &&
          period.end.getTime() <= c.end.getTime(),
      ) ??
      null,
    [fullCycle, cycles, period],
  );

  let tokens: number | null;
  if (fullCycle) {
    tokens = fullCycle.tokens;
  } else if (covered) {
    tokens = sumDaysInPeriod(billedDays.byDate, period);
  } else {
    tokens = details?.totals?.totalTokens ?? null;
  }

  let overage: number | null = null;
  if (limit != null && fullCycle) {
    overage = Math.max(0, fullCycle.tokens - limit);
  } else if (covered && overageDays != null) {
    overage = Math.round(sumDaysInPeriod(overageDays, period));
  }

  return (
    <TumUsageCard
      period={period}
      containingCycle={containingCycle}
      tokens={tokens}
      overage={overage}
      limit={limit}
    />
  );
}

/**
 * The billed tokens-under-management position for the selected period.
 * A full billing cycle renders its complete billing position: headline
 * figures (used / included / overage) with a slim two-segment meter
 * underneath. A custom range renders range-scoped figures only — the
 * allowance, percentage, and meter are cycle-level concepts and hide rather
 * than compare days of traffic against a monthly number.
 */
function TumUsageCard({
  period,
  containingCycle,
  tokens,
  overage,
  limit,
}: {
  // The effective page period the card describes.
  period: BillingPeriod;
  // The cycle the period sits inside — the title's context suffix on custom
  // ranges. Null when the range spans cycle boundaries.
  containingCycle: BillingCycle | null;
  // Billed tokens within the period; null while a fallback total loads.
  tokens: number | null;
  // Billed overage tokens within the period; null when it can't be
  // attributed (no allowance, or the range escapes the billed daily data).
  overage: number | null;
  // Contracted monthly allowance; null when the org has no contracted cap.
  limit: number | null;
}): JSX.Element {
  const fullCycle = period.cycle;

  const rangeTitle = (
    <>
      {formatPeriodLabel(period)}
      {containingCycle && (
        <span className="font-normal">
          {" "}
          · {formatCycleName(containingCycle)}
        </span>
      )}
    </>
  );

  const overageValue = overage != null ? overage.toLocaleString() : "—";
  const meter =
    fullCycle != null && limit != null
      ? meterShares(fullCycle.tokens, limit)
      : null;
  const usedPercent =
    fullCycle != null && limit != null && limit > 0
      ? (fullCycle.tokens / limit) * 100
      : null;

  return (
    <div className="border-border rounded-lg border p-4">
      <div className="text-muted-foreground mb-3 text-sm font-medium">
        {fullCycle ? formatCycleName(fullCycle) : rangeTitle}
      </div>
      <div className="flex flex-wrap items-start gap-x-10 gap-y-3">
        <Stat
          label="Tokens Managed"
          value={tokens != null ? tokens.toLocaleString() : null}
        />
        {fullCycle != null && (
          <Stat
            label="Included allowance"
            value={limit != null ? limit.toLocaleString() : "No limit"}
            tone={limit != null ? "success" : undefined}
          />
        )}
        {limit != null && (
          <Stat
            label="Overage"
            value={overageValue}
            tone={overage != null && overage > 0 ? "warning" : undefined}
            hint={overageHintFor(fullCycle, overage)}
          />
        )}
        <AverageStat period={period} tokens={tokens} />
        {usedPercent != null && (
          <span
            className={cn(
              "text-muted-foreground ml-auto self-end text-xs tabular-nums",
              usedPercent > 100 && "text-warning",
            )}
          >
            {Math.round(usedPercent).toLocaleString()}% of allowance
          </span>
        )}
      </div>

      {meter != null && (
        <div className="bg-muted mt-4 flex h-2 w-full gap-0.5 overflow-hidden rounded-full">
          <div
            className="bg-success-default h-full rounded-full transition-all duration-300"
            style={{ width: `${meter.included}%` }}
          />
          {meter.overage > 0 && (
            <div
              className="bg-warning-default h-full rounded-full transition-all duration-300"
              style={{ width: `${meter.overage}%` }}
            />
          )}
        </div>
      )}
    </div>
  );
}
