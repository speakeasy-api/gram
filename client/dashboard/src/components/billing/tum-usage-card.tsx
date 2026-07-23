import { useState } from "react";
import { Info } from "lucide-react";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { ToggleButton } from "@/components/ui/toggle-button";
import { type BillingCycle } from "./billing-cycles";

const HOUR_MS = 60 * 60 * 1000;

// The selectable units for the average-rate stat.
type AverageUnit = "hour" | "day" | "week";

const AVERAGE_UNITS: { unit: AverageUnit; ms: number }[] = [
  { unit: "hour", ms: HOUR_MS },
  { unit: "day", ms: 24 * HOUR_MS },
  { unit: "week", ms: 7 * 24 * HOUR_MS },
];

// The window the cycle's averages describe: the full cycle once it closes,
// the elapsed portion while it's active (a whole-cycle denominator would
// dilute the rate with days that haven't happened yet). Clamped to at least
// an hour so a just-opened cycle doesn't extrapolate absurd rates.
function averagingWindowMs(cycle: BillingCycle, now: number): number {
  const end = cycle.current
    ? Math.min(now, cycle.end.getTime())
    : cycle.end.getTime();
  return Math.max(end - cycle.start.getTime(), HOUR_MS);
}

// One average-rate figure with a unit switcher: the cycle's tokens expressed
// per hour / day / week, defaulting to per day.
function AverageStat({ cycle }: { cycle: BillingCycle }): JSX.Element {
  const [unit, setUnit] = useState<AverageUnit>("day");
  const windowMs = averagingWindowMs(cycle, Date.now());
  const unitMs = AVERAGE_UNITS.find((u) => u.unit === unit)?.ms ?? HOUR_MS;
  const average = Math.round((cycle.tokens * unitMs) / windowMs);
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
            cycle.current
              ? "Averaged over the elapsed portion of the active cycle."
              : "Averaged over the full cycle."
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
      <span className="text-xl font-semibold tabular-nums">
        {average.toLocaleString()}
      </span>
    </div>
  );
}

// One headline figure in the TUM usage card. The tone ties the number to its
// meter segment: green for the included allowance, amber for overage.
function Stat({
  label,
  value,
  tone,
}: {
  label: string;
  value: string;
  tone?: "success" | "warning";
}): JSX.Element {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-muted-foreground text-xs">{label}</span>
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
    </div>
  );
}

/**
 * The billed tokens-under-management position for one billing cycle: headline
 * figures (used / included / extra) with a slim two-segment meter underneath.
 * The numbers live up here as first-class stats rather than as labels crammed
 * around the meter, so nothing clips or collides at extreme overage ratios.
 */
export function TumUsageCard({
  cycle,
  limit,
  label,
}: {
  // The billing cycle whose position the card shows — billed tokens plus the
  // time window its per-unit averages are computed over.
  cycle: BillingCycle;
  // Contracted monthly allowance; null when the org has no contracted cap.
  limit: number | null;
  // Which billing cycle these figures describe. Essential when the page is
  // scoped to a custom range: the card's whole-cycle totals exceed the
  // range's totals, and the label is what keeps that from reading as a bug.
  label: string;
}): JSX.Element {
  const tokens = cycle.tokens;
  const overage = limit != null ? Math.max(0, tokens - limit) : 0;
  // The meter spans max(usage, allowance): under the cap it reads as a
  // fill-toward-the-limit bar; over it, the amber overage segment grows.
  const scale = limit != null ? Math.max(tokens, limit) : 0;
  const includedShare =
    scale > 0 ? (Math.min(tokens, limit ?? 0) / scale) * 100 : 0;
  const overageShare = scale > 0 ? (overage / scale) * 100 : 0;
  const usedPercent =
    limit != null && limit > 0 ? (tokens / limit) * 100 : null;

  return (
    <div className="border-border rounded-lg border p-4">
      <div className="text-muted-foreground mb-3 text-sm font-medium">
        {label}
      </div>
      <div className="flex flex-wrap items-start gap-x-10 gap-y-3">
        <Stat label="Tokens Managed" value={tokens.toLocaleString()} />
        <Stat
          label="Included allowance"
          value={limit != null ? limit.toLocaleString() : "No limit"}
          tone={limit != null ? "success" : undefined}
        />
        {limit != null && (
          <Stat
            label="Overage"
            value={overage.toLocaleString()}
            tone={overage > 0 ? "warning" : undefined}
          />
        )}
        <AverageStat cycle={cycle} />
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

      {limit != null && (
        <div className="bg-muted mt-4 flex h-2 w-full gap-0.5 overflow-hidden rounded-full">
          <div
            className="bg-success-default h-full rounded-full transition-all duration-300"
            style={{ width: `${includedShare}%` }}
          />
          {overage > 0 && (
            <div
              className="bg-warning-default h-full rounded-full transition-all duration-300"
              style={{ width: `${overageShare}%` }}
            />
          )}
        </div>
      )}
    </div>
  );
}
