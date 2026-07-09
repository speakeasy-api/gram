import { cn } from "@/lib/utils";

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
          "font-display text-2xl font-thin tracking-[-0.015em] tabular-nums",
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
  tokens,
  limit,
  label,
}: {
  // Billed TUM tokens for the selected cycle.
  tokens: number;
  // Contracted monthly allowance; null when the org has no contracted cap.
  limit: number | null;
  // Which billing cycle these figures describe. Essential when the page is
  // scoped to a custom range: the card's whole-cycle totals exceed the
  // range's totals, and the label is what keeps that from reading as a bug.
  label: string;
}): JSX.Element {
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
        <Stat label="Tokens consumed" value={tokens.toLocaleString()} />
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
