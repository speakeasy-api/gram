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
  type BillingCycle,
  type BillingPeriod,
  type PeriodFigures,
  formatCycleName,
  formatPeriodLabel,
} from "./billing-cycles";
import { tumDetailsQuery } from "./tum-queries";

const HOUR_MS = 60 * 60 * 1000;

// Keyboard-reachable info tooltip beside a stat label: a real button so the
// explanation is reachable by keyboard focus, not just pointer hover.
function InfoHint({
  label,
  tooltip,
}: {
  label: string;
  tooltip: string;
}): JSX.Element {
  return (
    <SimpleTooltip tooltip={tooltip}>
      <button
        type="button"
        aria-label={label}
        className="inline-flex shrink-0 cursor-help"
      >
        <Info aria-hidden className="size-3" />
      </button>
    </SimpleTooltip>
  );
}

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
// Which cycle is active is the server's call — a sealed cycle always
// averages over its full window, so a skewed browser clock can't shrink a
// complete cycle's denominator. Clamped to at least an hour so a just-opened
// window doesn't extrapolate absurd rates.
function averagingWindow(
  period: BillingPeriod,
  now: number,
): { ms: number; partial: boolean } {
  const clampToNow = period.cycle ? period.cycle.current : true;
  const end = clampToNow
    ? Math.min(now, period.end.getTime())
    : period.end.getTime();
  return {
    ms: Math.max(end - period.start.getTime(), HOUR_MS),
    partial: clampToNow && period.end.getTime() > now,
  };
}

// One average-rate figure with a unit switcher: the period's tokens expressed
// per hour / day / week, defaulting to per day.
function AverageStat({
  period,
  tokens,
  failed,
}: {
  period: BillingPeriod;
  // Billed tokens within the period; null while the total is still loading
  // or when the fallback request failed.
  tokens: number | null;
  // Whether the total is null because the fallback request failed.
  failed: boolean;
}): JSX.Element {
  const [unit, setUnit] = useState<AverageUnit>("day");
  const avgWindow = averagingWindow(period, Date.now());
  const unitMs = AVERAGE_UNITS.find((u) => u.unit === unit)?.ms ?? HOUR_MS;
  const average =
    tokens != null ? Math.round((tokens * unitMs) / avgWindow.ms) : null;

  let value: JSX.Element;
  if (average != null) {
    value = (
      <span className="font-display text-3xl font-thin">
        {average.toLocaleString()}
      </span>
    );
  } else if (failed) {
    value = <span className="font-display text-3xl font-thin">—</span>;
  } else {
    value = <Skeleton className="h-9 w-24" />;
  }

  return (
    <div className="flex flex-col gap-0.5">
      {/* Pinned to the label line height so the switcher's chrome overflows
          the label row instead of inflating it — this stat's label and value
          stay aligned with the neighboring Stats. */}
      <span className="text-eyebrow flex h-4 items-center gap-1">
        Avg per
        {/* Bordered so the units read as a clickable segmented control
            rather than as part of the label text. */}
        <span className="border-border flex items-center border p-0.5">
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
        <InfoHint
          label="How the average is calculated"
          tooltip={
            avgWindow.partial
              ? "Averaged over the elapsed portion of the selected period."
              : "Averaged over the full selected period."
          }
        />
      </span>
      {value}
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
      <span className="text-eyebrow flex h-4 items-center gap-1">
        {label}
        {hint && <InfoHint label={`About ${label}`} tooltip={hint} />}
      </span>
      {value != null ? (
        <span
          className={cn(
            "font-display text-3xl font-thin",
            tone === "success" && "text-default-success",
            tone === "warning" && "text-default-warning",
          )}
        >
          {value}
        </span>
      ) : (
        <Skeleton className="h-9 w-28" />
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
 * Resolves the usage card's numbers for the effective period from the shared
 * PeriodFigures (the same object the chart headline and details table read),
 * falling back to the analytics total from the shared details query for
 * ranges the billed daily series can't answer.
 */
export function PeriodUsageCard({
  period,
  cycles,
  figures,
  limit,
}: {
  period: BillingPeriod;
  cycles: BillingCycle[];
  // The billed answers for the period, resolved once in the section.
  figures: PeriodFigures;
  limit: number | null;
}): JSX.Element {
  const client = useGramContext();
  const organization = useOrganization();
  const fullCycle = period.cycle;
  // Fallback total for ranges the billed daily series can't answer — the
  // same request (identical query key) the chart and details table share.
  const { data: details, isError } = useQuery(
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

  const tokens = figures.tokens ?? details?.totals?.totalTokens ?? null;
  // Only the fallback path depends on the details request; when it failed
  // with nothing cached, say so instead of skeleting forever.
  const failed = figures.tokens == null && !details && isError;

  return (
    <TumUsageCard
      period={period}
      containingCycle={containingCycle}
      tokens={tokens}
      overage={figures.overage}
      limit={limit}
      failed={failed}
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
  failed,
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
  // Whether the tokens total is unavailable because its request failed —
  // rendered as an explicit dash, never as an endless loading skeleton.
  failed: boolean;
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

  let tokensValue: string | null;
  if (tokens != null) {
    tokensValue = tokens.toLocaleString();
  } else if (failed) {
    tokensValue = "—";
  } else {
    tokensValue = null;
  }
  const tokensHint = failed
    ? "Couldn't load usage for this range. Try again shortly."
    : undefined;

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
    <div className="border-border border p-4">
      <div className="text-muted-foreground mb-3 text-sm font-medium">
        {fullCycle ? formatCycleName(fullCycle) : rangeTitle}
      </div>
      <div className="flex flex-wrap items-start gap-x-10 gap-y-3">
        <Stat label="Tokens Managed" value={tokensValue} hint={tokensHint} />
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
        <AverageStat period={period} tokens={tokens} failed={failed} />
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
        <div className="bg-muted mt-4 flex h-2 w-full gap-0.5 overflow-hidden">
          <div
            className="bg-success-default h-full transition-all duration-300"
            style={{ width: `${meter.included}%` }}
          />
          {meter.overage > 0 && (
            <div
              className="bg-warning-default h-full transition-all duration-300"
              style={{ width: `${meter.overage}%` }}
            />
          )}
        </div>
      )}
    </div>
  );
}
