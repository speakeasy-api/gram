import { Card } from "@/components/ui/card";
import { UsageMeter } from "@/components/ui/progress";
import { StatTile } from "@/components/ui/stat-tile";
import { cn } from "@/lib/utils";

// Headline figures render at a compact size (not StatTile's default huge
// serif) so three stats fit comfortably in one row without wrapping or
// clipping at extreme overage ratios.
const STAT_VALUE_CLASS = "text-2xl sm:text-2xl";

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
  const usedPercent =
    limit != null && limit > 0 ? (tokens / limit) * 100 : null;

  return (
    <Card>
      <div className="text-muted-foreground text-sm font-medium">{label}</div>
      <div className="flex flex-wrap items-start gap-x-10 gap-y-3">
        <StatTile
          label="Tokens consumed"
          value={tokens.toLocaleString()}
          valueClassName={STAT_VALUE_CLASS}
        />
        <StatTile
          label="Included allowance"
          value={limit != null ? limit.toLocaleString() : "No limit"}
          valueClassName={STAT_VALUE_CLASS}
        />
        {limit != null && (
          <StatTile
            label="Overage"
            value={overage.toLocaleString()}
            tone={overage > 0 ? "warning" : "default"}
            valueClassName={STAT_VALUE_CLASS}
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
        <UsageMeter used={tokens} included={limit} overageUsed={overage} />
      )}
    </Card>
  );
}
